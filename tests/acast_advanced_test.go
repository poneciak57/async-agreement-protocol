package tests

import (
	"async-agreement-protocol-3/services"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func setupACastAdvanced(n, f int) (*services.Network[services.ACastMessage[string]], []*services.ServiceManager[services.ACastMessage[string], string], func()) {
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

func TestACast_LargePayload(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastAdvanced(n, f)
	defer cleanup()

	// Create a large payload (e.g., 1MB)
	val := strings.Repeat("A", 1024*1024)
	senderID := 1
	msg := services.NewACastMessage(val, senderID)

	// Broadcast
	for _, sm := range managers {
		sm.Inbox() <- msg
	}

	// Verify
	for i, sm := range managers {
		select {
		case res := <-sm.Result():
			if len(res) != len(val) {
				t.Errorf("Node %d delivered wrong length: got %d, want %d", i+1, len(res), len(val))
			}
			if res != val {
				t.Errorf("Node %d delivered wrong content", i+1)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Node %d timed out waiting for result", i+1)
		}
	}
}

func TestACast_MultipleBroadcasts(t *testing.T) {
	n, f := 4, 1
	_, managers, cleanup := setupACastAdvanced(n, f)
	defer cleanup()

	count := 5
	senderID := 1

	for k := 0; k < count; k++ {
		val := "Value-" + string(rune(k))
		msg := services.NewACastMessage(val, senderID)

		// Broadcast
		for _, sm := range managers {
			sm.Inbox() <- msg
		}

		// Verify
		for i, sm := range managers {
			select {
			case res := <-sm.Result():
				if res != val {
					t.Errorf("Node %d delivered wrong value in iter %d: got %v, want %v", i+1, k, res, val)
				}
			case <-time.After(2 * time.Second):
				t.Errorf("Node %d timed out waiting for result in iter %d", i+1, k)
			}
		}
	}
}
