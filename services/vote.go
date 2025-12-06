package services

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// VotePayloadType defines the type of data carried in an A-Cast for Vote
type VotePayloadType int

const (
	Vote_Input VotePayloadType = iota
	Vote_Vote1
	Vote_Revote
)

// VotePayload is the data structure serialized into the A-Cast value string
type VotePayload struct {
	Type   VotePayloadType
	Sender int
	Bit    int   // 0 or 1
	Set    []int // A_i or B_i
	Round  int   // Added Round to payload
}

func (p VotePayload) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func ParseVotePayload(s string) (*VotePayload, error) {
	var p VotePayload
	err := json.Unmarshal([]byte(s), &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// VoteMsgType distinguishes between direct messages and A-Cast wrapper messages
type VoteMsgType int

const (
	Vote_ACast VoteMsgType = iota
)

// VoteMessage is the main message type exchanged by Vote services
type VoteMessage struct {
	Type     VoteMsgType
	ACastMsg *ACastMessage[string] `json:",omitempty"`
}

// VoteResult is the output of the Vote service
type VoteResult struct {
	Value int // 0 or 1, or -1 for null
	Conf  int // 0, 1, 2
	Round int
}

type voteRoundState struct {
	round int

	// Phase 1
	receivedInputs map[int]int // sender -> bit
	myA            []int
	sentVote1      bool

	// Phase 2
	receivedVote1 map[int]struct {
		Set []int
		Bit int
	}
	myB        []int
	sentRevote bool

	// Phase 3
	receivedRevote map[int]struct {
		Set []int
		Bit int
	}
	myC []int

	finished bool
}

func newVoteRoundState(round int) *voteRoundState {
	return &voteRoundState{
		round:          round,
		receivedInputs: make(map[int]int),
		receivedVote1: make(map[int]struct {
			Set []int
			Bit int
		}),
		receivedRevote: make(map[int]struct {
			Set []int
			Bit int
		}),
	}
}

// VoteService implements the Vote protocol
type VoteService struct {
	id     int
	n      int
	t      int
	logger zerolog.Logger

	acast *AcastService[string]

	mu sync.Mutex

	rounds map[int]*voteRoundState
}

func NewVoteService(id, n, t int, logLevel zerolog.Level) *VoteService {
	logger := log.With().
		Str("layer", "Vote").
		Int("node_id", id).
		Logger().
		Level(logLevel)

	return &VoteService{
		id:     id,
		n:      n,
		t:      t,
		logger: logger,
		rounds: make(map[int]*voteRoundState),
		acast:  NewAcastService[string](id, n, t, logLevel),
	}
}

func (s *VoteService) StartRound(round int, inputBit int, ctx ServiceContext[VoteMessage, VoteResult]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info().Int("round", round).Int("input", inputBit).Msg("Starting Vote Protocol Round")

	state := s.getRoundState(round)
	if state.finished {
		return
	}

	// Faza 1: A-Cast "INPUT: (i, x_i)"
	payload := VotePayload{
		Type:   Vote_Input,
		Sender: s.id,
		Bit:    inputBit,
		Round:  round,
	}
	s.startACast(payload, ctx)

	// Check progress immediately in case we already have messages buffered/received
	s.checkProgress(state, ctx)
}

func (s *VoteService) getRoundState(round int) *voteRoundState {
	if _, ok := s.rounds[round]; !ok {
		s.rounds[round] = newVoteRoundState(round)
	}
	return s.rounds[round]
}

func (s *VoteService) OnMessage(msg VoteMessage, ctx ServiceContext[VoteMessage, VoteResult]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.Type == Vote_ACast && msg.ACastMsg != nil {
		adapter := &voteAcastAdapter{
			vote: s,
			ctx:  ctx,
		}
		s.acast.OnMessage(*msg.ACastMsg, adapter)
	}
}

// voteAcastAdapter adapts ServiceContext[VoteMessage, VoteResult] to ServiceContext[ACastMessage[string], string]
type voteAcastAdapter struct {
	vote *VoteService
	ctx  ServiceContext[VoteMessage, VoteResult]
}

func (a *voteAcastAdapter) Broadcast(msg ACastMessage[string]) {
	a.ctx.Broadcast(VoteMessage{
		Type:     Vote_ACast,
		ACastMsg: &msg,
	})
}

func (a *voteAcastAdapter) SendResult(res string) {
	payload, err := ParseVotePayload(res)
	if err != nil {
		a.vote.logger.Error().Err(err).Msg("Failed to parse Vote payload")
		return
	}
	a.vote.processDeliveredPayload(payload, a.ctx)
}

func (s *VoteService) processDeliveredPayload(p *VotePayload, ctx ServiceContext[VoteMessage, VoteResult]) {
	// Assumes s.mu is locked

	// Get or create state for the round
	state := s.getRoundState(p.Round)

	if state.finished {
		return
	}

	sender := p.Sender

	switch p.Type {
	case Vote_Input:
		state.receivedInputs[sender] = p.Bit
	case Vote_Vote1:
		state.receivedVote1[sender] = struct {
			Set []int
			Bit int
		}{Set: p.Set, Bit: p.Bit}
	case Vote_Revote:
		state.receivedRevote[sender] = struct {
			Set []int
			Bit int
		}{Set: p.Set, Bit: p.Bit}
	}

	s.checkProgress(state, ctx)
}

func (s *VoteService) checkProgress(state *voteRoundState, ctx ServiceContext[VoteMessage, VoteResult]) {
	// Helper to get keys from receivedInputs
	allInputs := make([]int, 0, len(state.receivedInputs))
	for k := range state.receivedInputs {
		allInputs = append(allInputs, k)
	}

	// Phase 1 Check
	if !state.sentVote1 {
		if len(state.receivedInputs) >= s.n-s.t {
			// Form A_i
			var A []int
			zeros := 0
			ones := 0
			for sender, bit := range state.receivedInputs {
				A = append(A, sender)
				if bit == 0 {
					zeros++
				} else {
					ones++
				}
			}
			sort.Ints(A)
			state.myA = A

			// Majority bit
			myVote1 := 0
			if ones > zeros {
				myVote1 = 1
			}

			state.sentVote1 = true
			s.logger.Info().Int("round", state.round).Ints("A_set", state.myA).Int("vote1", myVote1).Msg("Broadcasting VOTE1")

			payload := VotePayload{
				Type:   Vote_Vote1,
				Sender: s.id,
				Bit:    myVote1,
				Set:    state.myA,
				Round:  state.round,
			}
			s.startACast(payload, ctx)
		}
	}

	// Phase 2 Check
	// Identify valid VOTE1 messages (A_j subset of allInputs)
	validVote1s := make([]int, 0)
	for sender, data := range state.receivedVote1 {
		if isSubset(data.Set, allInputs) {
			validVote1s = append(validVote1s, sender)
		}
	}

	if state.sentVote1 && !state.sentRevote {
		if len(validVote1s) >= s.n-s.t {
			// Form B_i
			var B []int
			zeros := 0
			ones := 0

			for _, sender := range validVote1s {
				B = append(B, sender)
				data := state.receivedVote1[sender]
				if data.Bit == 0 {
					zeros++
				} else {
					ones++
				}
			}
			sort.Ints(B)
			state.myB = B

			// Majority bit
			myVote2 := 0
			if ones > zeros {
				myVote2 = 1
			}

			state.sentRevote = true
			s.logger.Info().Int("round", state.round).Ints("B_set", state.myB).Int("vote2", myVote2).Msg("Broadcasting REVOTE")

			payload := VotePayload{
				Type:   Vote_Revote,
				Sender: s.id,
				Bit:    myVote2,
				Set:    state.myB,
				Round:  state.round,
			}
			s.startACast(payload, ctx)
		}
	}

	// Phase 3 Check (Decision)
	// Identify valid REVOTE messages (B_j subset of validVote1s)
	validRevotes := make([]int, 0)
	for sender, data := range state.receivedRevote {
		if isSubset(data.Set, validVote1s) {
			validRevotes = append(validRevotes, sender)
		}
	}

	if state.sentRevote {
		if len(validRevotes) >= s.n-s.t {
			sort.Ints(validRevotes)
			state.myC = validRevotes

			// Decision Logic

			// 1. Check if all votes in B_i are equal to sigma
			firstVal := -1
			allSameB := true
			for _, j := range state.myB {
				val := state.receivedVote1[j].Bit
				if firstVal == -1 {
					firstVal = val
				} else if firstVal != val {
					allSameB = false
					break
				}
			}

			if allSameB && firstVal != -1 {
				s.finish(state, firstVal, 2, ctx)
				return
			}

			// 2. Check if all revotes in C_i are equal to sigma
			firstValC := -1
			allSameC := true
			for _, j := range state.myC {
				val := state.receivedRevote[j].Bit
				if firstValC == -1 {
					firstValC = val
				} else if firstValC != val {
					allSameC = false
					break
				}
			}

			if allSameC && firstValC != -1 {
				s.finish(state, firstValC, 1, ctx)
				return
			}

			// 3. Else
			s.finish(state, -1, 0, ctx)
		}
	}
}

func (s *VoteService) finish(state *voteRoundState, val, conf int, ctx ServiceContext[VoteMessage, VoteResult]) {
	state.finished = true
	s.logger.Info().Int("round", state.round).Int("value", val).Int("conf", conf).Msg("Vote Finished")
	ctx.SendResult(VoteResult{Value: val, Conf: conf, Round: state.round})
}

func (s *VoteService) startACast(payload VotePayload, ctx ServiceContext[VoteMessage, VoteResult]) {
	val := payload.String()
	msg := NewACastMessage(val, s.id)

	// Send MSG to all (via Broadcast)
	ctx.Broadcast(VoteMessage{
		Type:     Vote_ACast,
		ACastMsg: &msg,
	})

	// Also handle it locally
	adapter := &voteAcastAdapter{
		vote: s,
		ctx:  ctx,
	}
	s.acast.OnMessage(msg, adapter)
}
