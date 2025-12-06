package services

import (
	"encoding/json"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ABAMsgType defines the type of message for ABA
type ABAMsgType int

const (
	ABA_Vote ABAMsgType = iota
	ABA_ICC
	ABA_Complete
)

// ABAMessage is the wrapper message for ABA
type ABAMessage struct {
	Type        ABAMsgType
	Round       int
	VoteMsg     *VoteMessage          `json:",omitempty"`
	ICCMsg      *ICCMessage           `json:",omitempty"`
	CompleteMsg *ACastMessage[string] `json:",omitempty"`
}

// CompletePayload is the data for the COMPLETE message
type CompletePayload struct {
	Sender int
	Value  int
}

func (p CompletePayload) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func ParseCompletePayload(s string) (*CompletePayload, error) {
	var p CompletePayload
	err := json.Unmarshal([]byte(s), &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ABAService implements the Asynchronous Byzantine Agreement protocol
type ABAService struct {
	id       int
	n        int
	t        int
	estimate int
	round    int

	cp *CertificationProtocol

	// Sub-services
	vote          *VoteService
	icc           map[int]*ICCService
	acastComplete *AcastService[string]

	// State for current round
	voteResult *VoteResult
	iccResult  *ICCResult
	// Global State
	completeCounts       map[int]map[int]bool // value -> set of senders
	decided              bool
	decision             int
	hasBroadcastComplete bool

	// Buffers
	futureMsgs map[int][]ABAMessage

	mu     sync.Mutex
	logger zerolog.Logger
}

func NewABAService(id, n, t, initialEstimate int, cp *CertificationProtocol, logLevel zerolog.Level) *ABAService {
	logger := log.With().
		Str("layer", "ABA").
		Int("node_id", id).
		Logger().
		Level(logLevel)

	s := &ABAService{
		id:             id,
		n:              n,
		t:              t,
		estimate:       initialEstimate,
		round:          0,
		cp:             cp,
		vote:           NewVoteService(id, n, t, logLevel),
		icc:            make(map[int]*ICCService),
		completeCounts: make(map[int]map[int]bool),
		futureMsgs:     make(map[int][]ABAMessage),
		logger:         logger,
		acastComplete:  NewAcastService[string](id, n, t, logLevel),
	}

	return s
}

func (s *ABAService) Start(ctx ServiceContext[ABAMessage, int]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info().Int("estimate", s.estimate).Msg("Starting ABA")
	s.startRound(1, ctx)
}

func (s *ABAService) startRound(r int, ctx ServiceContext[ABAMessage, int]) {
	s.round = r
	s.voteResult = nil
	s.iccResult = nil

	s.logger.Info().Int("round", r).Int("estimate", s.estimate).Msg("Starting Round")

	// Initialize sub-services for this round
	// s.vote is already initialized
	s.icc[r] = NewICCService(s.id, s.n, s.t, r, s.cp, s.logger.GetLevel())

	// Start Vote
	voteAdapter := &abaVoteAdapter{aba: s, ctx: ctx, round: r}
	s.vote.StartRound(r, s.estimate, voteAdapter)

	// Start ICC
	iccAdapter := &abaICCAdapter{aba: s, ctx: ctx, round: r}
	s.icc[r].Start(iccAdapter)

	// Process buffered messages for this round
	if msgs, ok := s.futureMsgs[r]; ok {
		s.logger.Info().Int("round", r).Int("count", len(msgs)).Msg("Processing buffered messages")
		for _, msg := range msgs {
			s.dispatchMessage(msg, ctx)
		}
		delete(s.futureMsgs, r)
	}
}

func (s *ABAService) OnMessage(msg ABAMessage, ctx ServiceContext[ABAMessage, int]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.Type == ABA_Complete {
		s.dispatchMessage(msg, ctx)
		return
	}

	// Vote messages are handled by the single VoteService which manages rounds internally.
	if msg.Type == ABA_Vote {
		// Buffer future round messages to ensure they are processed only when the ABA protocol
		// advances to that round. This maintains consistency with other round-based services.
		if msg.Round > s.round {
			s.futureMsgs[msg.Round] = append(s.futureMsgs[msg.Round], msg)
			return
		}

		s.dispatchMessage(msg, ctx)
		return
	}

	if msg.Round > s.round {
		// Future message, buffer
		s.futureMsgs[msg.Round] = append(s.futureMsgs[msg.Round], msg)
		return
	}

	// Current or old round message
	s.dispatchMessage(msg, ctx)
}

func (s *ABAService) dispatchMessage(msg ABAMessage, ctx ServiceContext[ABAMessage, int]) {
	// Assumes lock is held
	switch msg.Type {
	case ABA_Vote:
		if s.vote != nil && msg.VoteMsg != nil {
			adapter := &abaVoteAdapter{aba: s, ctx: ctx, round: msg.Round}
			s.vote.OnMessage(*msg.VoteMsg, adapter)
		}
	case ABA_ICC:
		if svc, ok := s.icc[msg.Round]; ok && msg.ICCMsg != nil {
			adapter := &abaICCAdapter{aba: s, ctx: ctx, round: msg.Round}
			svc.OnMessage(*msg.ICCMsg, adapter)
		}
	case ABA_Complete:
		if msg.CompleteMsg != nil {
			adapter := &abaCompleteAdapter{aba: s, ctx: ctx}
			s.acastComplete.OnMessage(*msg.CompleteMsg, adapter)
		}
	}
}

func (s *ABAService) checkRoundProgress(ctx ServiceContext[ABAMessage, int]) {
	// Assumes lock is held
	if s.voteResult != nil && s.iccResult != nil {
		// Both phases done for this round
		voteVal := s.voteResult.Value
		voteConf := s.voteResult.Conf
		coinVal := s.iccResult.Coin

		s.logger.Info().
			Int("round", s.round).
			Int("vote_val", voteVal).
			Int("vote_conf", voteConf).
			Int("coin_val", coinVal).
			Msg("Round Completed")

		// Logic
		if s.decided {
			s.estimate = s.decision
		} else {
			if voteConf == 2 {
				// Strong majority
				s.estimate = voteVal
				// A-Cast COMPLETE if not already done
				if !s.hasBroadcastComplete {
					s.broadcastComplete(voteVal, ctx)
					s.hasBroadcastComplete = true
				}
			} else if voteConf == 1 {
				// Weak majority
				s.estimate = voteVal
			} else {
				// No majority
				s.estimate = coinVal
			}
		}

		// Move to next round
		s.startRound(s.round+1, ctx)
	}
}

func (s *ABAService) broadcastComplete(val int, ctx ServiceContext[ABAMessage, int]) {
	payload := CompletePayload{
		Sender: s.id,
		Value:  val,
	}
	strVal := payload.String()
	msg := NewACastMessage(strVal, s.id)

	// Broadcast
	ctx.Broadcast(ABAMessage{
		Type:        ABA_Complete,
		CompleteMsg: &msg,
	})

	// Local delivery
	adapter := &abaCompleteAdapter{aba: s, ctx: ctx}
	s.acastComplete.OnMessage(msg, adapter)
}

func (s *ABAService) handleCompleteDelivery(valStr string, ctx ServiceContext[ABAMessage, int]) {
	// Assumes lock is held
	payload, err := ParseCompletePayload(valStr)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse Complete payload")
		return
	}

	if s.completeCounts[payload.Value] == nil {
		s.completeCounts[payload.Value] = make(map[int]bool)
	}
	s.completeCounts[payload.Value][payload.Sender] = true

	count := len(s.completeCounts[payload.Value])
	s.logger.Info().Int("value", payload.Value).Int("count", count).Msg("Received COMPLETE")

	if count >= s.t+1 && !s.decided {
		s.decided = true
		s.decision = payload.Value
		s.logger.Info().Int("decision", s.decision).Msg("DECIDED")
		ctx.SendResult(s.decision)

		// Even if we decide based on receiving enough COMPLETE messages, we must ensure
		// we broadcast COMPLETE ourselves to help other nodes reach the threshold.
		if !s.hasBroadcastComplete {
			s.broadcastComplete(s.decision, ctx)
			s.hasBroadcastComplete = true
		}
	}
}

// Adapters

type abaVoteAdapter struct {
	aba   *ABAService
	ctx   ServiceContext[ABAMessage, int]
	round int
}

func (a *abaVoteAdapter) Broadcast(msg VoteMessage) {
	a.ctx.Broadcast(ABAMessage{
		Type:    ABA_Vote,
		Round:   a.round,
		VoteMsg: &msg,
	})
}

func (a *abaVoteAdapter) SendResult(res VoteResult) {
	// Assumes lock is held by the caller (aba.OnMessage or aba.Start)
	if a.round == a.aba.round {
		a.aba.voteResult = &res
		a.aba.checkRoundProgress(a.ctx)
	}
}

type abaICCAdapter struct {
	aba   *ABAService
	ctx   ServiceContext[ABAMessage, int]
	round int
}

func (a *abaICCAdapter) Broadcast(msg ICCMessage) {
	a.ctx.Broadcast(ABAMessage{
		Type:   ABA_ICC,
		Round:  a.round,
		ICCMsg: &msg,
	})
}

func (a *abaICCAdapter) SendResult(res ICCResult) {
	// Assumes lock is held by the caller (aba.OnMessage or aba.Start)
	if a.round == a.aba.round {
		a.aba.iccResult = &res
		a.aba.checkRoundProgress(a.ctx)
	}
}

type abaCompleteAdapter struct {
	aba *ABAService
	ctx ServiceContext[ABAMessage, int]
}

func (a *abaCompleteAdapter) Broadcast(msg ACastMessage[string]) {
	a.ctx.Broadcast(ABAMessage{
		Type:        ABA_Complete,
		CompleteMsg: &msg,
	})
}

func (a *abaCompleteAdapter) SendResult(res string) {
	a.aba.handleCompleteDelivery(res, a.ctx)
}
