package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type MessageType int

const (
	MSG MessageType = iota
	ECHO
	READY
)

func (m MessageType) String() string {
	switch m {
	case MSG:
		return "MSG"
	case ECHO:
		return "ECHO"
	case READY:
		return "READY"
	default:
		return "UNKNOWN"
	}
}

type ACastMessage[T any] struct {
	Type MessageType
	UUID string // Unique identifier for the message instance
	Val  T
	From int // Immediate sender
}

func NewACastMessage[T any](val T, from int) ACastMessage[T] {
	// Generate a unique ID based on content, sender and timestamp
	// This is a simple way to generate a unique ID for the broadcast instance
	// In a real system, you might want something more robust or provided by the user
	hashInput := fmt.Sprintf("%v-%d-%d", val, from, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(hashInput))
	uuid := hex.EncodeToString(hash[:])

	return ACastMessage[T]{
		Type: MSG,
		UUID: uuid,
		Val:  val,
		From: from,
	}
}

type ACastInstance[T comparable] struct {
	receivedEcho  map[T]map[int]bool
	receivedReady map[T]map[int]bool
	sentEcho      bool
	sentReady     bool
	delivered     bool
}

func NewACastInstance[T comparable]() *ACastInstance[T] {
	return &ACastInstance[T]{
		receivedEcho:  make(map[T]map[int]bool),
		receivedReady: make(map[T]map[int]bool),
	}
}

type AcastService[T comparable] struct {
	id        int
	n         int
	t         int
	instances map[string]*ACastInstance[T]
	logger    zerolog.Logger
}

func NewAcastService[T comparable](id, n, t int, logLevel zerolog.Level) *AcastService[T] {
	logger := log.With().
		Str("layer", "ACAST").
		Int("node_id", id).
		Logger().
		Level(logLevel)

	return &AcastService[T]{
		id:        id,
		n:         n,
		t:         t,
		instances: make(map[string]*ACastInstance[T]),
		logger:    logger,
	}
}

func (a *AcastService[T]) getInstance(uuid string) *ACastInstance[T] {
	if _, ok := a.instances[uuid]; !ok {
		a.instances[uuid] = NewACastInstance[T]()
	}
	return a.instances[uuid]
}

func (a *AcastService[T]) OnMessage(msg ACastMessage[T], ctx ServiceContext[ACastMessage[T], T]) {
	inst := a.getInstance(msg.UUID)

	// inst.mu.Lock()
	// defer inst.mu.Unlock()

	if inst.delivered {
		return
	}

	// Helper to add to set
	addToSet := func(m map[T]map[int]bool, val T, from int) int {
		if _, ok := m[val]; !ok {
			m[val] = make(map[int]bool)
		}
		m[val][from] = true
		return len(m[val])
	}

	switch msg.Type {
	case MSG:
		// On Receive MSG(val) from Sender:
		// if not sent_echo:
		//     Send ECHO(val) to all processes
		//     sent_echo = True

		// For MSG type, we assume it's the initial broadcast.
		// The UUID uniquely identifies this broadcast instance.

		if !inst.sentEcho {
			inst.sentEcho = true
			// Unlock before broadcast to avoid holding lock during network op
			// But we need to be careful. Here we use defer Unlock, so we hold it.
			// Since Broadcast is async (goroutine in Network), it's fine.

			a.logger.Debug().Msgf("Received MSG from %d, broadcasting ECHO", msg.From)
			ctx.Broadcast(ACastMessage[T]{
				Type: ECHO,
				UUID: msg.UUID,
				Val:  msg.Val,
				From: a.id,
			})
		}

	case ECHO:
		// On Receive ECHO(val) from process 'j':
		// Add 'j' to received_echo[val]
		// if size(received_echo[val]) >= n - t and not sent_ready:
		//     Send READY(val) to all processes
		//     sent_ready = True

		count := addToSet(inst.receivedEcho, msg.Val, msg.From)
		threshold := a.n - a.t

		if count >= threshold && !inst.sentReady {
			inst.sentReady = true

			a.logger.Debug().Str("uuid", msg.UUID).Msgf("Threshold ECHO reached (%d), broadcasting READY", count)
			ctx.Broadcast(ACastMessage[T]{
				Type: READY,
				UUID: msg.UUID,
				Val:  msg.Val,
				From: a.id,
			})
		}

	case READY:
		// On Receive READY(val) from process 'j':
		// Add 'j' to received_ready[val]
		// if size(received_ready[val]) >= t + 1 and not sent_ready:
		//     Send READY(val) to all processes
		//     sent_ready = True
		// if size(received_ready[val]) >= 2*t + 1 and not delivered:
		//     output_value = val
		//     delivered = True
		//     Trigger event "A-Cast Complete" returns val

		count := addToSet(inst.receivedReady, msg.Val, msg.From)
		a.logger.Debug().Str("uuid", msg.UUID).Int("count", count).Int("from", msg.From).Msg("Received READY vote")

		// Early trigger
		if count >= a.t+1 && !inst.sentReady {
			inst.sentReady = true
			a.logger.Debug().Str("uuid", msg.UUID).Msgf("Threshold READY (early) reached (%d), broadcasting READY", count)

			ctx.Broadcast(ACastMessage[T]{
				Type: READY,
				UUID: msg.UUID,
				Val:  msg.Val,
				From: a.id,
			})
		}

		// Delivery condition
		if count >= 2*a.t+1 && !inst.delivered {
			inst.delivered = true
			// Optimization: Clear maps to save memory
			inst.receivedEcho = nil
			inst.receivedReady = nil

			a.logger.Info().Msgf("A-Cast Complete: Delivered value %v", msg.Val)
			ctx.SendResult(msg.Val)
		}
	}
}
