package tests

import (
	"async-agreement-protocol-3/services"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// setupACastCluster creates a cluster of n nodes with tolerance f.
// It returns the network, the service managers, and a cleanup function.
func setupACastCluster(n, f int) (*services.Network[services.ACastMessage[string]], []*services.ServiceManager[services.ACastMessage[string], string], func()) {
	network := services.NewNetwork[services.ACastMessage[string]]()
	managers := make([]*services.ServiceManager[services.ACastMessage[string], string], n)

	for i := 0; i < n; i++ {
		id := i + 1
		svc := services.NewAcastService[string](id, n, f, zerolog.Disabled)
		managers[i] = services.NewServiceManager[services.ACastMessage[string], string](svc, network)
		network.Register(id, managers[i].Inbox())
		managers[i].Start()
	}

	cleanup := func() {
		for _, sm := range managers {
			sm.Stop()
		}
	}

	return network, managers, cleanup
}

func TestACast_HappyPath(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastCluster(n, f)
	defer cleanup()

	// Test Data
	val := "TestValue"
	senderID := 1
	msg := services.NewACastMessage(val, senderID)

	// Simulate Sender broadcasting MSG to all nodes
	for _, sm := range managers {
		sm.Inbox() <- msg
	}

	// Verify Delivery
	for i, sm := range managers {
		select {
		case res := <-sm.Result():
			if res != val {
				t.Errorf("Node %d delivered wrong value: got %v, want %v", i+1, res, val)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("Node %d timed out waiting for result", i+1)
		}
	}
}

func TestACast_PartialBroadcast(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastCluster(n, f)
	defer cleanup()

	val := "PartialValue"
	senderID := 1
	msg := services.NewACastMessage(val, senderID)

	// Simulate Sender broadcasting MSG to only 3 nodes (Node 1, 2, 3)
	// Node 4 does not receive MSG directly.
	for i := 0; i < 3; i++ {
		managers[i].Inbox() <- msg
	}

	// Verify Delivery on ALL nodes (including Node 4)
	for i, sm := range managers {
		select {
		case res := <-sm.Result():
			if res != val {
				t.Errorf("Node %d delivered wrong value: got %v, want %v", i+1, res, val)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("Node %d timed out waiting for result", i+1)
		}
	}
}

func TestACast_InsufficientBroadcast(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastCluster(n, f)
	defer cleanup()

	val := "InsufficientValue"
	senderID := 1
	msg := services.NewACastMessage(val, senderID)

	// Simulate Sender broadcasting MSG to only 2 nodes
	for i := 0; i < 2; i++ {
		managers[i].Inbox() <- msg
	}

	// Verify NO Delivery
	time.Sleep(500 * time.Millisecond)
	for i, sm := range managers {
		select {
		case res := <-sm.Result():
			t.Errorf("Node %d delivered value unexpectedly: %v", i+1, res)
		default:
			// Expected behavior
		}
	}
}

func TestACast_ManyMessages(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastCluster(n, f)
	defer cleanup()

	numMessages := 50
	senderID := 1

	var wg sync.WaitGroup
	wg.Add(n * numMessages)

	// Start a goroutine for each node to collect results
	for i := 0; i < n; i++ {
		go func(nodeIdx int, sm *services.ServiceManager[services.ACastMessage[string], string]) {
			receivedCount := 0
			for receivedCount < numMessages {
				select {
				case <-sm.Result():
					receivedCount++
					wg.Done()
				case <-time.After(5 * time.Second):
					t.Errorf("Node %d timed out waiting for messages. Received %d/%d", nodeIdx+1, receivedCount, numMessages)
					return
				}
			}
		}(i, managers[i])
	}

	// Send messages
	for i := 0; i < numMessages; i++ {
		val := fmt.Sprintf("Msg-%d", i)
		msg := services.NewACastMessage(val, senderID)
		// Broadcast to all
		for _, sm := range managers {
			sm.Inbox() <- msg
		}
		// Small delay to not overwhelm channel buffer immediately if it was small (it's 1000 so it's fine)
	}

	wg.Wait()
}

func TestACast_ConcurrentBroadcasts(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastCluster(n, f)
	defer cleanup()

	numSenders := 4 // All nodes broadcast
	msgsPerSender := 10
	totalMessages := numSenders * msgsPerSender

	var wg sync.WaitGroup
	wg.Add(n * totalMessages)

	// Start collectors
	for i := 0; i < n; i++ {
		go func(nodeIdx int, sm *services.ServiceManager[services.ACastMessage[string], string]) {
			receivedCount := 0
			for receivedCount < totalMessages {
				select {
				case <-sm.Result():
					receivedCount++
					wg.Done()
				case <-time.After(5 * time.Second):
					t.Errorf("Node %d timed out. Received %d/%d", nodeIdx+1, receivedCount, totalMessages)
					return
				}
			}
		}(i, managers[i])
	}

	// Start senders
	var senderWg sync.WaitGroup
	senderWg.Add(numSenders)
	for s := 0; s < numSenders; s++ {
		go func(senderId int) {
			defer senderWg.Done()
			for m := 0; m < msgsPerSender; m++ {
				val := fmt.Sprintf("Sender-%d-Msg-%d", senderId, m)
				// Use a unique ID for each message across all senders to avoid collision if Src wasn't part of key
				// But ACastInstance key is (Src, Id), so (1, 0) is different from (2, 0).
				// So we can reuse message IDs 0..9 for each sender.
				msg := services.NewACastMessage(val, senderId)
				// Broadcast to all
				for _, sm := range managers {
					sm.Inbox() <- msg
				}
				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			}
		}(s + 1)
	}

	senderWg.Wait()
	wg.Wait()
}

// MockServiceContext for testing OnMessage directly
type MockServiceContext[TMsg any, TRes any] struct{}

func (m *MockServiceContext[TMsg, TRes]) Broadcast(msg TMsg)  {}
func (m *MockServiceContext[TMsg, TRes]) SendResult(res TRes) {}

func TestACast_RaceCondition_NilMapAccess(t *testing.T) {
	// This test attempts to reproduce a race condition where maps are set to nil
	// while another goroutine tries to access them.

	n, f := 4, 1
	svc1 := services.NewAcastService[string](1, n, f, zerolog.Disabled)
	svc2 := services.NewAcastService[string](2, n, f, zerolog.Disabled)
	ctx := &MockServiceContext[services.ACastMessage[string], string]{}

	// Run many iterations to increase chance of race
	for iter := 0; iter < 1000; iter++ {
		val := fmt.Sprintf("RaceValue-%d", iter)
		senderID := 1
		baseMsg := services.NewACastMessage(val, senderID)
		uuid := baseMsg.UUID

		var wg sync.WaitGroup
		wg.Add(2)
		// Pre-send ECHO messages to set up state
		for i := 1; i <= 3; i++ {
			msg := services.ACastMessage[string]{
				Type: services.READY,
				UUID: uuid,
				Val:  val,
				From: i,
			}
			svc1.OnMessage(msg, ctx)
			svc1.OnMessage(msg, ctx)
		}

		// Goroutine 1: Tries to access maps (ECHO messages) in service 1
		go func() {
			defer wg.Done()
			for i := 1; i <= 3; i++ {
				msg := services.ACastMessage[string]{
					Type: services.ECHO,
					UUID: uuid,
					Val:  val,
					From: i,
				}
				svc1.OnMessage(msg, ctx)
			}
		}()

		// Goroutine 2: Tries to access maps (ECHO messages) in service 2
		go func() {
			defer wg.Done()
			for i := 1; i <= 4; i++ {
				msg := services.ACastMessage[string]{
					Type: services.ECHO,
					UUID: uuid,
					Val:  val,
					From: i,
				}
				svc2.OnMessage(msg, ctx)
			}
		}()

		wg.Wait()
	}
}
