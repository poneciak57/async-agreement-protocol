package services

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ICCPayloadType defines the type of data carried in an A-Cast for ICC
type ICCPayloadType int

const (
	ICC_Attach ICCPayloadType = iota
	ICC_Accept
	ICC_ReconstructEnabled
	ICC_FinalSets
)

// ICCPayload is the data structure serialized into the A-Cast value string
type ICCPayload struct {
	Type ICCPayloadType
	// Data fields
	SetT   []int `json:",omitempty"` // For Attach
	SetA   []int `json:",omitempty"` // For Accept
	SetH   []int `json:",omitempty"` // For FinalSets
	SetS   []int `json:",omitempty"` // For FinalSets
	Sender int   `json:",omitempty"` // Added Sender field
}

func (p ICCPayload) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func ParseICCPayload(s string) (*ICCPayload, error) {
	var p ICCPayload
	err := json.Unmarshal([]byte(s), &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ICCMsgType distinguishes between direct messages and A-Cast wrapper messages
type ICCMsgType int

const (
	ICC_IVSS ICCMsgType = iota
	ICC_ACast
)

// ICCMessage is the main message type exchanged by ICC services
type ICCMessage struct {
	Type ICCMsgType

	// For IVSS messages
	IVSSMsg *IVSSMessage `json:",omitempty"`

	// For A-Cast messages
	ACastMsg *ACastMessage[string] `json:",omitempty"`
}

// ICCResult is the output of the ICC service
type ICCResult struct {
	Coin int // 0 or 1
}

// ICCService implements the Inferable Common Coin protocol
type ICCService struct {
	id     int
	n      int
	t      int
	round  int
	u      int // Modulo for coin calculation
	logger zerolog.Logger

	ivss  *IVSSService
	acast *AcastService[string]

	// State
	mu sync.Mutex

	// Step 1 & 2: Sharing and T sets
	// dealer -> count of completed secrets (needs n to be complete for that dealer)
	completedSecretsCount map[int]int
	// dealer -> secretIdx -> bool
	completedSecrets map[int]map[int]bool

	myT        []int
	sentAttach bool
	receivedT  map[int][]int // from -> T set

	// Step 3: A sets
	myA        []int
	sentAccept bool
	receivedA  map[int][]int // from -> A set

	// Step 4: S sets and Reconstruction Trigger
	myS             []int
	sentReconstruct bool
	receivedS       map[int][]int // from -> S set

	myH []int // The H set I broadcasted

	// Step 5: Reconstruction
	// dealer -> secretIdx -> value
	reconstructedValues map[int]map[int]*big.Int

	// Step 6: Decision
	// received (H, S) pairs
	receivedFinalSets []struct {
		From int
		H    []int
		S    []int
	}

	finished bool
}

func NewICCService(id, n, t, round int, cp *CertificationProtocol, logLevel zerolog.Level) *ICCService {
	logger := log.With().
		Str("layer", "ICC").
		Int("node_id", id).
		Int("round", round).
		Logger().
		Level(logLevel)

	// u = ceil(0.87 * n)
	u := int(math.Ceil(0.87 * float64(n)))

	icc := &ICCService{
		id:                    id,
		n:                     n,
		t:                     t,
		round:                 round,
		u:                     u,
		logger:                logger,
		completedSecretsCount: make(map[int]int),
		completedSecrets:      make(map[int]map[int]bool),
		receivedT:             make(map[int][]int),
		receivedA:             make(map[int][]int),
		receivedS:             make(map[int][]int),
		reconstructedValues:   make(map[int]map[int]*big.Int),
		receivedFinalSets: make([]struct {
			From int
			H    []int
			S    []int
		}, 0),
	}

	// Initialize IVSS service
	icc.ivss = NewIVSSService(id, n, t, cp, logLevel)

	// Initialize A-Cast service
	icc.acast = NewAcastService[string](id, n, t, logLevel)

	return icc
}

// Start initiates the ICC protocol
func (s *ICCService) Start(ctx ServiceContext[ICCMessage, ICCResult]) {
	s.logger.Info().Msg("Starting ICC Protocol")

	// 1. Choose n random secrets and share them
	for j := 1; j <= s.n; j++ {
		secret, _ := rand.Int(rand.Reader, big.NewInt(1000)) // Random secret
		instanceID := s.getInstanceID(s.id, j)

		// Create adapter for IVSS context
		adapter := &ivssContextAdapter{
			icc: s,
			ctx: ctx,
		}

		err := s.ivss.StartSharing(instanceID, secret, adapter)
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to start sharing")
		}
	}
}

func (s *ICCService) OnMessage(msg ICCMessage, ctx ServiceContext[ICCMessage, ICCResult]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finished {
		return
	}

	if msg.Type == ICC_IVSS {
		if msg.IVSSMsg != nil {
			adapter := &ivssContextAdapter{
				icc: s,
				ctx: ctx,
			}
			s.ivss.OnMessage(*msg.IVSSMsg, adapter)
		}
	} else if msg.Type == ICC_ACast {
		if msg.ACastMsg != nil {
			adapter := &iccAcastAdapter{
				icc: s,
				ctx: ctx,
			}
			// Delegate to AcastService
			s.acast.OnMessage(*msg.ACastMsg, adapter)
		}
	}

	s.checkProgress(ctx)
}

// iccAcastAdapter adapts ServiceContext[ICCMessage, ICCResult] to ServiceContext[ACastMessage[string], string]
type iccAcastAdapter struct {
	icc *ICCService
	ctx ServiceContext[ICCMessage, ICCResult]
}

func (a *iccAcastAdapter) Broadcast(msg ACastMessage[string]) {
	a.ctx.Broadcast(ICCMessage{
		Type:     ICC_ACast,
		ACastMsg: &msg,
	})
}

func (a *iccAcastAdapter) SendResult(res string) {
	// res is the delivered value (payload string)
	payload, err := ParseICCPayload(res)
	if err != nil {
		a.icc.logger.Error().Err(err).Msg("Failed to parse ICC payload from A-Cast")
		return
	}
	a.icc.processDeliveredPayload(payload, a.ctx)
}

// ivssContextAdapter adapts ServiceContext[ICCMessage, ICCResult] to ServiceContext[IVSSMessage, IVSSResult]
type ivssContextAdapter struct {
	icc *ICCService
	ctx ServiceContext[ICCMessage, ICCResult]
}

func (a *ivssContextAdapter) Broadcast(msg IVSSMessage) {
	a.ctx.Broadcast(ICCMessage{
		Type:    ICC_IVSS,
		IVSSMsg: &msg,
	})
}

func (a *ivssContextAdapter) SendResult(res IVSSResult) {
	a.icc.handleIVSSResult(res, a.ctx)
}

func (s *ICCService) handleIVSSResult(res IVSSResult, ctx ServiceContext[ICCMessage, ICCResult]) {
	// Parse InstanceID to get dealer and secretIdx
	// Format: "ICC-{round}-{dealer}-{secretIdx}"
	var round, dealer, secretIdx int
	_, err := fmt.Sscanf(res.InstanceID, "ICC-%d-%d-%d", &round, &dealer, &secretIdx)
	if err != nil {
		// Not an ICC instance or bad format
		return
	}

	if round != s.round {
		return
	}

	if res.Type == "SHARING_COMPLETE" {
		// Step 2: Build T_i
		if s.completedSecrets[dealer] == nil {
			s.completedSecrets[dealer] = make(map[int]bool)
		}
		if !s.completedSecrets[dealer][secretIdx] {
			s.completedSecrets[dealer][secretIdx] = true
			s.completedSecretsCount[dealer]++
			s.logger.Debug().Int("dealer", dealer).Int("count", s.completedSecretsCount[dealer]).Msg("Secret Completed")

			// Check if we have all n secrets for this dealer
			if s.completedSecretsCount[dealer] == s.n {
				// Add dealer to T_i (conceptually)
				// We check this in checkProgress
			}
		}
	} else if res.Type == "RECONSTRUCTED" {
		// Step 5: Store reconstructed value
		if s.reconstructedValues[dealer] == nil {
			s.reconstructedValues[dealer] = make(map[int]*big.Int)
		}
		s.reconstructedValues[dealer][secretIdx] = res.Secret
	}

	s.checkProgress(ctx)
}

func (s *ICCService) checkProgress(ctx ServiceContext[ICCMessage, ICCResult]) {
	// Step 2: Check if we can form T_i and A-Cast it
	if !s.sentAttach {
		// T_i = set of dealers j such that we completed all n secrets from j
		var T []int
		for dealer, count := range s.completedSecretsCount {
			if count == s.n {
				T = append(T, dealer)
			}
		}

		if len(T) >= s.n-s.t {
			s.myT = T
			s.sentAttach = true
			sort.Ints(s.myT)

			// A-Cast "attach T_i to i"
			s.logger.Info().Ints("T_set", s.myT).Msg("Broadcasting Attach T")
			payload := ICCPayload{
				Type:   ICC_Attach,
				SetT:   s.myT,
				Sender: s.id, // Added Sender field
			}
			s.startACast(payload, ctx)
		}
	}

	// Step 3: Check if we can form A_i and A-Cast it
	if !s.sentAccept {
		// A_i = set of j such that we received "attach T_j" and T_j is subset of T_i
		// Note: We need T_i to be fixed (which happens when sentAttach is true)
		if s.sentAttach {
			var A []int
			for j, Tj := range s.receivedT {
				isSub := isSubset(Tj, s.myT)
				if isSub {
					A = append(A, j)
				} else {
					s.logger.Debug().Int("from", j).Ints("T_j", Tj).Ints("myT", s.myT).Msg("Rejecting T_j (not subset)")
				}
			}

			if len(A) >= s.n-s.t {
				s.myA = A
				s.sentAccept = true
				sort.Ints(s.myA)

				// A-Cast "i accepts A_i"
				payload := ICCPayload{
					Type:   ICC_Accept,
					SetA:   s.myA,
					Sender: s.id,
				}
				s.startACast(payload, ctx)
			}
		}
	}

	// Step 4: Check if we can form S_i and A-Cast Reconstruct Enabled
	if !s.sentReconstruct {
		if s.sentAccept {
			var S []int
			for j, Aj := range s.receivedA {
				if isSubset(Aj, s.myA) {
					S = append(S, j)
				}
			}

			if len(S) >= s.n-s.t {
				s.myS = S
				s.sentReconstruct = true
				sort.Ints(s.myS)

				// A-Cast "Reconstruct Enabled" and (H_i, S_i)
				// H_i is current A_i
				s.myH = s.myA // Snapshot

				payload := ICCPayload{
					Type:   ICC_FinalSets,
					SetH:   s.myH,
					SetS:   s.myS,
					Sender: s.id,
				}
				s.startACast(payload, ctx)

				// Also start reconstruction for required secrets
				s.startReconstruction(ctx)
			}
		}
	}

	// Step 6: Check for decision
	s.checkDecision(ctx)
}

func (s *ICCService) startACast(payload ICCPayload, ctx ServiceContext[ICCMessage, ICCResult]) {
	val := payload.String()
	msg := NewACastMessage(val, s.id)

	// Send MSG to all (via Broadcast)
	// The A-Cast logic starts by broadcasting MSG
	ctx.Broadcast(ICCMessage{
		Type:     ICC_ACast,
		ACastMsg: &msg,
	})

	// Also handle it locally as if received
	adapter := &iccAcastAdapter{
		icc: s,
		ctx: ctx,
	}
	s.acast.OnMessage(msg, adapter)
}

func (s *ICCService) startReconstruction(ctx ServiceContext[ICCMessage, ICCResult]) {
	// Participate in IVSS-R(x_{k,j}) for every k in T_j and j in A_i.
	// We actively start the reconstruction process for these secrets.

	// For each j in A_i (my A_i):
	//   For each k in T_j (the T set of j):
	//     Start Reconstruction for secret x_{k,j} (Dealer k, secret index j)

	for _, j := range s.myA {
		Tj, ok := s.receivedT[j]
		if !ok {
			continue // Should not happen if j is in A_i
		}
		for _, k := range Tj {
			instanceID := s.getInstanceID(k, j)

			adapter := &ivssContextAdapter{
				icc: s,
				ctx: ctx,
			}
			// We call StartReconstruction.
			// In IVSS, StartReconstruction can be called by anyone.
			s.ivss.StartReconstruction(instanceID, adapter)
		}
	}
}

func (s *ICCService) checkDecision(ctx ServiceContext[ICCMessage, ICCResult]) {
	if s.finished {
		return
	}

	// Check if we have a valid (H, S) pair from someone
	for _, finalSet := range s.receivedFinalSets {
		H := finalSet.H
		S := finalSet.S

		// Conditions: H <= A_i, S <= S_i
		if s.sentAccept && s.sentReconstruct { // Ensure we have A_i and S_i
			if isSubset(H, s.myA) && isSubset(S, s.myS) {
				// Check if all values for processes in H are computed
				allComputed := true
				hasZero := false

				for _, j := range H {
					// Compute v_j
					// v_j = sum(y_{k,j}) mod u for k in T_j
					Tj, ok := s.receivedT[j]
					if !ok {
						allComputed = false
						break
					}

					sum := big.NewInt(0)
					complete := true
					for _, k := range Tj {
						// Get reconstructed secret y_{k,j} (Dealer k, secret j)
						if s.reconstructedValues[k] == nil || s.reconstructedValues[k][j] == nil {
							complete = false
							break
						}
						sum.Add(sum, s.reconstructedValues[k][j])
					}

					if !complete {
						allComputed = false
						break
					}

					// v_j = sum mod u
					uBig := big.NewInt(int64(s.u))
					vj := new(big.Int).Mod(sum, uBig)

					if vj.Cmp(big.NewInt(0)) == 0 {
						hasZero = true
					}
				}

				if allComputed {
					// Output
					coin := 1
					if hasZero {
						coin = 0
					}

					s.finished = true
					s.logger.Info().Int("coin", coin).Msg("ICC Finished")
					ctx.SendResult(ICCResult{Coin: coin})
					return
				}
			}
		}
	}
}

func (s *ICCService) processDeliveredPayload(p *ICCPayload, ctx ServiceContext[ICCMessage, ICCResult]) {
	sender := p.Sender

	switch p.Type {
	case ICC_Attach:
		s.receivedT[sender] = p.SetT

	case ICC_Accept:
		s.receivedA[sender] = p.SetA

	case ICC_FinalSets:
		s.receivedFinalSets = append(s.receivedFinalSets, struct {
			From int
			H    []int
			S    []int
		}{
			From: sender,
			H:    p.SetH,
			S:    p.SetS,
		})
	}

	s.checkProgress(ctx)
}

func (s *ICCService) getInstanceID(dealer, secretIdx int) string {
	return fmt.Sprintf("ICC-%d-%d-%d", s.round, dealer, secretIdx)
}

// Utils

func isSubset(sub, super []int) bool {
	// Assumes sorted slices
	// Or just use map for O(N)
	// Since N is small, O(N^2) is fine, but let's be efficient.
	// Using maps is easier.

	superMap := make(map[int]bool)
	for _, v := range super {
		superMap[v] = true
	}

	for _, v := range sub {
		if !superMap[v] {
			return false
		}
	}
	return true
}
