package services

import (
	"async-agreement-protocol-3/utils"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// IVSSPayloadType defines the type of data carried in an A-Cast for IVSS
type IVSSPayloadType int

const (
	Payload_Equal IVSSPayloadType = iota
	Payload_MSet
	Payload_Reveal
	Payload_Ready
)

// IVSSPayload is the data structure serialized into the A-Cast value string
type IVSSPayload struct {
	InstanceID string
	Type       IVSSPayloadType
	// Data fields
	EqualPair    [2]int            `json:",omitempty"`
	MSet         []int             `json:",omitempty"`
	RevealPoly   *utils.Polynomial `json:",omitempty"`
	RevealSender int               `json:",omitempty"`
}

func (p IVSSPayload) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func ParseIVSSPayload(s string) (*IVSSPayload, error) {
	var p IVSSPayload
	err := json.Unmarshal([]byte(s), &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// IVSSMsgType distinguishes between direct messages and A-Cast wrapper messages
type IVSSMsgType int

const (
	IVSS_Direct IVSSMsgType = iota
	IVSS_ACast
)

// DirectMsgType defines types of point-to-point messages
type DirectMsgType int

const (
	Direct_Share DirectMsgType = iota
	Direct_Point
)

// IVSSMessage is the main message type exchanged by IVSS services
type IVSSMessage struct {
	Type IVSSMsgType

	// For Direct Messages
	DirectType DirectMsgType     `json:",omitempty"`
	To         int               `json:",omitempty"` // Intended recipient
	From       int               `json:",omitempty"`
	InstanceID string            `json:",omitempty"`
	Poly       *utils.Polynomial `json:",omitempty"` // For Share
	Point      *big.Int          `json:",omitempty"` // For Point
	PointIdx   int               `json:",omitempty"` // j for f_k(j)

	// For A-Cast Messages
	ACastMsg *ACastMessage[string] `json:",omitempty"`
}

// IVSSResult is the output of the IVSS service
type IVSSResult struct {
	InstanceID string
	Type       string // "SHARING_COMPLETE" or "RECONSTRUCTED"
	Secret     *big.Int
	MSet       []int
	Poly       *utils.Polynomial
}

// IVSSInstance holds the state for one IVSS protocol instance
type IVSSInstance struct {
	id     string
	dealer int
	mu     sync.Mutex

	// Sharing Phase
	receivedPoly     *utils.Polynomial
	receivedPoints   map[int]*big.Int
	earlyPoints      map[int]*big.Int // Points received before the share
	consistentPeers  map[int]bool
	completedEquals  map[[2]int]bool // Tracks "EQUAL:(i,j)" completions
	mSet             []int
	pendingMSet      []int // Store M-Set if received before all EQUALs
	sharingCompleted bool

	// Reconstruction Phase
	reconstructedPolys map[int]*utils.Polynomial
	readyToComplete    map[int]bool
	reconstructed      bool
	secret             *big.Int
}

func NewIVSSInstance(id string, dealer int) *IVSSInstance {
	return &IVSSInstance{
		id:                 id,
		dealer:             dealer,
		receivedPoints:     make(map[int]*big.Int),
		earlyPoints:        make(map[int]*big.Int),
		consistentPeers:    make(map[int]bool),
		completedEquals:    make(map[[2]int]bool),
		reconstructedPolys: make(map[int]*utils.Polynomial),
		readyToComplete:    make(map[int]bool),
	}
}

// IVSSService implements the IVSS protocol
type IVSSService struct {
	id     int
	n      int
	t      int
	acast  *AcastService[string]
	cp     *CertificationProtocol
	logger zerolog.Logger

	instances map[string]*IVSSInstance
	mu        sync.Mutex
}

func NewIVSSService(id, n, t int, cp *CertificationProtocol, logLevel zerolog.Level) *IVSSService {
	logger := log.With().
		Str("layer", "IVSS").
		Int("node_id", id).
		Logger().
		Level(logLevel)

	// Create internal A-Cast service
	// Note: The A-Cast service needs a context to broadcast.
	// We will provide an adapter context when calling OnMessage.
	acastSvc := NewAcastService[string](id, n, t, logLevel)

	return &IVSSService{
		id:        id,
		n:         n,
		t:         t,
		acast:     acastSvc,
		cp:        cp,
		logger:    logger,
		instances: make(map[string]*IVSSInstance),
	}
}

func (s *IVSSService) getInstance(id string, dealer int) *IVSSInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[id]; !ok {
		s.instances[id] = NewIVSSInstance(id, dealer)
	}
	return s.instances[id]
}

// StartSharing initiates the sharing phase (Dealer only)
func (s *IVSSService) StartSharing(instanceID string, secret *big.Int, ctx ServiceContext[IVSSMessage, IVSSResult]) error {
	// 1. Select random symmetric polynomial F(x,y)
	poly, err := utils.NewRandomSymmetricPolynomial(s.t, secret)
	if err != nil {
		return err
	}

	s.logger.Info().Str("instance", instanceID).Msg("Starting Sharing as Dealer")

	// 2. Send f_k(y) = F(k, y) to each process k
	for k := 1; k <= s.n; k++ {
		kBig := big.NewInt(int64(k))
		fk := poly.GetUnivariatePolynomial(kBig)

		// Send directly
		msg := IVSSMessage{
			Type:       IVSS_Direct,
			DirectType: Direct_Share,
			To:         k,
			From:       s.id,
			InstanceID: instanceID,
			Poly:       fk,
		}

		// If sending to self, we can optimize or just let it loopback if network supports it.
		// Assuming network supports loopback or we handle it.
		// For now, just broadcast with "To" field.
		ctx.Broadcast(msg)
	}
	return nil
}

// StartReconstruction initiates the reconstruction phase
func (s *IVSSService) StartReconstruction(instanceID string, ctx ServiceContext[IVSSMessage, IVSSResult]) error {
	inst := s.getInstance(instanceID, 0)
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if !inst.sharingCompleted {
		return fmt.Errorf("sharing not completed for instance %s", instanceID)
	}

	// Check if I am in M
	inM := false
	for _, id := range inst.mSet {
		if id == s.id {
			inM = true
			break
		}
	}

	if inM {
		// A-Cast my stored polynomial f_k
		payload := IVSSPayload{
			InstanceID:   inst.id,
			Type:         Payload_Reveal,
			RevealPoly:   inst.receivedPoly,
			RevealSender: s.id,
		}
		s.startACast(payload, ctx)
	} else {
		s.logger.Info().Msg("Not in M set, skipping reconstruction initiation")
	}
	return nil
}

// OnMessage handles incoming IVSS messages
func (s *IVSSService) OnMessage(msg IVSSMessage, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	if msg.Type == IVSS_ACast {
		// Pass to internal A-Cast service
		// We need an adapter for the context
		adapter := &acastContextAdapter{
			parentCtx: ctx,
			service:   s,
		}
		if msg.ACastMsg != nil {
			s.acast.OnMessage(*msg.ACastMsg, adapter)
		}
		return
	}

	// Handle Direct Messages
	if msg.To != s.id {
		return // Not for me
	}

	// TODO: Robust Dealer ID inference from InstanceID
	inst := s.getInstance(msg.InstanceID, msg.From)

	inst.mu.Lock()
	defer inst.mu.Unlock()

	switch msg.DirectType {
	case Direct_Share:
		// On Receive f_k from Dealer
		inst.receivedPoly = msg.Poly
		inst.dealer = msg.From // The sender of Share IS the dealer

		// Send point = f_k(j) to process j
		for j := 1; j <= s.n; j++ {
			jBig := big.NewInt(int64(j))
			val := msg.Poly.Evaluate(jBig)

			outMsg := IVSSMessage{
				Type:       IVSS_Direct,
				DirectType: Direct_Point,
				To:         j,
				From:       s.id,
				InstanceID: msg.InstanceID,
				Point:      val,
				PointIdx:   j,
			}
			ctx.Broadcast(outMsg)
		}

		// Process any early points
		for from, point := range inst.earlyPoints {
			s.processPoint(inst, from, point, ctx)
		}
		// Clear early points
		inst.earlyPoints = make(map[int]*big.Int)

	case Direct_Point:
		// On Receive point p_j from process j
		// Check consistency: received_poly(j) == p_j
		if inst.receivedPoly == nil {
			// We haven't received the poly from dealer yet.
			// Buffer the point
			inst.earlyPoints[msg.From] = msg.Point
			return
		}

		s.processPoint(inst, msg.From, msg.Point, ctx)
	}
}

func (s *IVSSService) startACast(payload IVSSPayload, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	// Create A-Cast message
	// We need a unique UUID for this A-Cast instance.
	// UUID = InstanceID + PayloadType + Data
	uuid := fmt.Sprintf("%s-%d-%v", payload.InstanceID, payload.Type, payload.EqualPair)
	if payload.Type == Payload_MSet {
		uuid = fmt.Sprintf("%s-MSET", payload.InstanceID)
	} else if payload.Type == Payload_Reveal {
		uuid = fmt.Sprintf("%s-REVEAL-%d", payload.InstanceID, s.id)
	} else if payload.Type == Payload_Ready {
		uuid = fmt.Sprintf("%s-READY-%d", payload.InstanceID, s.id)
	}

	acastMsg := NewACastMessage(payload.String(), s.id)
	acastMsg.UUID = uuid

	// We need to feed this into our internal AcastService to start the process (if we are the sender)
	wrapper := IVSSMessage{
		Type:     IVSS_ACast,
		ACastMsg: &acastMsg,
	}
	ctx.Broadcast(wrapper)
}

// OnACastDelivered is called when the internal A-Cast service delivers a value
func (s *IVSSService) OnACastDelivered(valStr string, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	payload, err := ParseIVSSPayload(valStr)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse IVSS payload")
		return
	}

	inst := s.getInstance(payload.InstanceID, 0) // Dealer ID might not be needed here if instance exists
	inst.mu.Lock()
	defer inst.mu.Unlock()

	switch payload.Type {
	case Payload_Equal:
		// Add to set of completed EQUALs
		inst.completedEquals[payload.EqualPair] = true
		s.checkCandidateSet(inst, ctx)

		// Check if pending M-Set is now valid
		if inst.pendingMSet != nil && !inst.sharingCompleted {
			if s.verifyMSet(inst, inst.pendingMSet) {
				inst.mSet = inst.pendingMSet
				inst.sharingCompleted = true
				inst.pendingMSet = nil // Clear pending

				s.logger.Info().Str("instance", inst.id).Msg("Sharing Complete (Delayed)")

				ctx.SendResult(IVSSResult{
					InstanceID: inst.id,
					Type:       "SHARING_COMPLETE",
					MSet:       inst.mSet,
					Poly:       inst.receivedPoly,
				})
			}
		}

	case Payload_MSet:
		// Dealer sent M Set. Store it as pending first.
		inst.pendingMSet = payload.MSet

		// Verify it immediately
		if s.verifyMSet(inst, payload.MSet) {
			inst.mSet = payload.MSet
			inst.sharingCompleted = true
			inst.pendingMSet = nil

			s.logger.Info().Str("instance", inst.id).Msg("Sharing Complete")

			ctx.SendResult(IVSSResult{
				InstanceID: inst.id,
				Type:       "SHARING_COMPLETE",
				MSet:       inst.mSet,
				Poly:       inst.receivedPoly,
			})
		} else {
			s.logger.Debug().Str("instance", inst.id).Msg("Received M-Set but not yet valid (waiting for EQUALs)")
		}

	case Payload_Reveal:
		// Reconstruction phase: received a polynomial
		inst.reconstructedPolys[payload.RevealSender] = payload.RevealPoly
		s.checkInterpolationSet(inst, ctx)

	case Payload_Ready:
		inst.readyToComplete[payload.RevealSender] = true
		if len(inst.readyToComplete) >= s.n-s.t && !inst.reconstructed {
			// Output Reconstructed Secret
			if inst.secret != nil {
				inst.reconstructed = true
				s.logger.Info().Str("instance", inst.id).Msgf("Reconstruction Complete. Secret: %v", inst.secret)

				ctx.SendResult(IVSSResult{
					InstanceID: inst.id,
					Type:       "RECONSTRUCTED",
					Secret:     inst.secret,
				})
			} else {
				s.logger.Warn().Str("instance", inst.id).Msg("Ready threshold reached but secret not yet interpolated")
			}
		}
	}
}

func (s *IVSSService) checkCandidateSet(inst *IVSSInstance, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	// Check if we can form a set M of size >= n-t
	// Condition: For every pair u, v in M: A-Cast "EQUAL:(u, v)" AND "EQUAL:(v, u)" are completed.
	// This ensures M is a clique of mutually consistent peers.

	if s.id != inst.dealer {
		return
	}

	if inst.sharingCompleted {
		return
	}

	// Candidates are all nodes 1..n
	candidates := make([]int, 0)
	for i := 1; i <= s.n; i++ {
		candidates = append(candidates, i)
	}

	// Build adjacency graph for consistency
	adj := make(map[int]map[int]bool)
	for _, u := range candidates {
		adj[u] = make(map[int]bool)
		for _, v := range candidates {
			if u == v {
				continue
			}
			// Check mutual consistency
			if inst.completedEquals[[2]int{u, v}] && inst.completedEquals[[2]int{v, u}] {
				// Check Certification Protocol
				if !s.cp.IsFaultyPair(u, v) {
					adj[u][v] = true
				}
			}
		}
	}

	// Try to find a clique of size n-t
	target := s.n - s.t
	mSet := findClique(candidates, target, adj)

	if mSet != nil {
		// Found a valid M-Set!
		sort.Ints(mSet)
		s.logger.Info().Str("instance", inst.id).Ints("MSet", mSet).Msg("Found valid M-Set, broadcasting")

		payload := IVSSPayload{
			InstanceID: inst.id,
			Type:       Payload_MSet,
			MSet:       mSet,
		}
		s.startACast(payload, ctx)
	}
}

func (s *IVSSService) verifyMSet(inst *IVSSInstance, mSet []int) bool {
	// Verify conditions:
	// 1. Size of nodes in M >= n-t
	// 2. For every pair i, j in M: A-Cast "EQUAL:(i, j)" is completed.

	if len(mSet) < s.n-s.t {
		return false
	}

	for i := 0; i < len(mSet); i++ {
		for j := i + 1; j < len(mSet); j++ {
			u, v := mSet[i], mSet[j]

			// Check if EQUAL is completed for {u, v}
			if !inst.completedEquals[[2]int{u, v}] {
				return false
			}
			if !inst.completedEquals[[2]int{v, u}] {
				return false
			}

			// Check Certification Protocol (FP)
			if s.cp.IsFaultyPair(u, v) {
				return false
			}
		}
	}

	return true
}

func (s *IVSSService) checkInterpolationSet(inst *IVSSInstance, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	// Check if we have n-2t polynomials that are consistent
	// We need to find a subset of received polynomials that are pairwise consistent.
	// Consistency check: poly_i(j) == poly_j(i)

	// Filter polynomials that are in M (if we enforce IS subset of M)
	// We need to know M.
	if inst.mSet == nil {
		return
	}

	// Extract nodes in M
	nodesInM := make(map[int]bool)
	for _, node := range inst.mSet {
		nodesInM[node] = true
	}

	// Candidates: nodes in M from whom we received a polynomial
	candidates := make([]int, 0)
	for k := range inst.reconstructedPolys {
		if nodesInM[k] {
			candidates = append(candidates, k)
		}
	}

	if len(candidates) < s.n-2*s.t {
		return
	}

	// Find a consistent subset of size n-2t
	// Similar clique problem.
	// Two polynomials P_u, P_v are consistent if P_u(v) == P_v(u).

	isConsistent := func(u, v int) bool {
		polyU := inst.reconstructedPolys[u]
		polyV := inst.reconstructedPolys[v]

		valUV := polyU.Evaluate(big.NewInt(int64(v)))
		valVU := polyV.Evaluate(big.NewInt(int64(u)))

		return valUV.Cmp(valVU) == 0
	}

	// Build adjacency for consistency
	adj := make(map[int]map[int]bool)
	for i := 0; i < len(candidates); i++ {
		u := candidates[i]
		adj[u] = make(map[int]bool)
		for j := 0; j < len(candidates); j++ {
			if i == j {
				continue
			}
			v := candidates[j]
			if isConsistent(u, v) {
				adj[u][v] = true
			} else {
				// Inconsistent! Add to FP
				s.cp.AddFaultyPair(u, v)
			}
		}
	}

	// Find clique of size n-2t
	target := s.n - 2*s.t
	if target <= 0 {
		target = 1
	} // Edge case

	validSet := findClique(candidates, target, adj)

	if validSet != nil {
		// Interpolate F(0,0)
		// We have polynomials f_i(y) = F(i, y).
		// We want F(0,0).
		// F(0,0) is the constant term of f_0(y) = F(0, y).
		// We can interpolate F(0, y) from values F(i, y) for i in validSet.
		// Actually, we can interpolate F(x, 0) from values F(i, 0).
		// F(i, 0) is the constant term of f_i(y).
		// Let s_i = f_i(0).
		// We have pairs (i, s_i). We want to interpolate S(x) such that S(i) = s_i.
		// Then secret = S(0).

		points := make([]*big.Int, len(validSet))
		values := make([]*big.Int, len(validSet))

		for idx, nodeID := range validSet {
			points[idx] = big.NewInt(int64(nodeID))
			// Constant term of f_nodeID(y) is f_nodeID(0)
			values[idx] = inst.reconstructedPolys[nodeID].Evaluate(big.NewInt(0))
		}

		secret := utils.InterpolateAtZero(points, values)
		inst.secret = secret

		// If successful:
		payload := IVSSPayload{
			InstanceID:   inst.id,
			Type:         Payload_Ready,
			RevealSender: s.id,
		}
		s.startACast(payload, ctx)
	}
}

// Adapter for AcastService
type acastContextAdapter struct {
	parentCtx ServiceContext[IVSSMessage, IVSSResult]
	service   *IVSSService
}

func (a *acastContextAdapter) Broadcast(msg ACastMessage[string]) {
	wrapper := IVSSMessage{
		Type:     IVSS_ACast,
		ACastMsg: &msg,
	}
	a.parentCtx.Broadcast(wrapper)
}

func (a *acastContextAdapter) SendResult(res string) {
	a.service.OnACastDelivered(res, a.parentCtx)
}

func (s *IVSSService) processPoint(inst *IVSSInstance, from int, point *big.Int, ctx ServiceContext[IVSSMessage, IVSSResult]) {
	jBig := big.NewInt(int64(from))
	myEval := inst.receivedPoly.Evaluate(jBig)

	if myEval.Cmp(point) == 0 {
		// Consistent!
		// A-Cast "EQUAL:(k, j)" -> k is me (s.id), j is msg.From
		payload := IVSSPayload{
			InstanceID: inst.id,
			Type:       Payload_Equal,
			EqualPair:  [2]int{s.id, from},
		}

		// Trigger A-Cast
		s.startACast(payload, ctx)
	} else {
		s.logger.Warn().Msgf("Inconsistent point from %d", from)
	}
}

// Helper to find a clique of size k in a graph defined by neighbors map
func findClique(nodes []int, k int, neighbors map[int]map[int]bool) []int {
	// Recursive backtracking
	var search func(candidates []int, currentClique []int) []int
	search = func(candidates []int, currentClique []int) []int {
		if len(currentClique) == k {
			return currentClique
		}
		// Pruning
		if len(currentClique)+len(candidates) < k {
			return nil
		}

		// Try including candidates[0]
		node := candidates[0]

		// Check if node is connected to all in currentClique
		canInclude := true
		for _, existing := range currentClique {
			if !neighbors[node][existing] {
				canInclude = false
				break
			}
		}

		if canInclude {
			newClique := append([]int(nil), currentClique...)
			newClique = append(newClique, node)
			res := search(candidates[1:], newClique)
			if res != nil {
				return res
			}
		}

		// Try excluding candidates[0]
		return search(candidates[1:], currentClique)
	}

	return search(nodes, []int{})
}
