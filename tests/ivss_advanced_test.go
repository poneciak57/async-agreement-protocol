package tests

import (
	"async-agreement-protocol-3/services"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func setupIVSSAdvanced(t *testing.T, n, f int) ([]*services.IVSSService, []*services.ServiceManager[services.IVSSMessage, services.IVSSResult], map[int]chan services.IVSSResult) {
	network := services.NewNetwork[services.IVSSMessage]()

	servicesList := make([]*services.IVSSService, n+1)
	managers := make([]*services.ServiceManager[services.IVSSMessage, services.IVSSResult], n+1)
	results := make(map[int]chan services.IVSSResult)

	for i := 1; i <= n; i++ {
		cp := services.NewCertificationProtocol()
		// Use DebugLevel to see what's happening, or InfoLevel to reduce noise
		svc := services.NewIVSSService(i, n, f, cp, zerolog.InfoLevel)
		servicesList[i] = svc

		mgr := services.NewServiceManager[services.IVSSMessage, services.IVSSResult](svc, network)
		managers[i] = mgr

		network.Register(i, mgr.Inbox())
		results[i] = make(chan services.IVSSResult, 100)

		mgr.Start()

		go func(id int, m *services.ServiceManager[services.IVSSMessage, services.IVSSResult]) {
			for res := range m.Result() {
				results[id] <- res
			}
		}(i, mgr)
	}

	return servicesList, managers, results
}

func TestIVSS_Basic(t *testing.T) {
	n := 4
	f := 1
	servicesList, managers, results := setupIVSSAdvanced(t, n, f)
	defer func() {
		for _, m := range managers {
			if m != nil {
				m.Stop()
			}
		}
	}()

	dealerID := 1
	secret := big.NewInt(42)
	instanceID := "IVSS-TEST-1"

	// Start Sharing
	t.Logf("Node %d starting sharing secret %v", dealerID, secret)
	err := servicesList[dealerID].StartSharing(instanceID, secret, managers[dealerID])
	if err != nil {
		t.Fatalf("Failed to start sharing: %v", err)
	}

	// Wait for Sharing Complete
	sharingCompleted := make(map[int]bool)
	timeout := time.After(5 * time.Second)

	for len(sharingCompleted) < n {
		select {
		case <-timeout:
			t.Fatalf("Timeout waiting for sharing complete. Completed: %v", sharingCompleted)
		default:
			// Check results
			for i := 1; i <= n; i++ {
				if sharingCompleted[i] {
					continue
				}
				select {
				case res := <-results[i]:
					if res.InstanceID == instanceID && res.Type == "SHARING_COMPLETE" {
						t.Logf("Node %d completed sharing", i)
						sharingCompleted[i] = true

						// Start Reconstruction immediately after sharing complete
						go func(id int) {
							err := servicesList[id].StartReconstruction(instanceID, managers[id])
							if err != nil {
								t.Errorf("Node %d failed to start reconstruction: %v", id, err)
							}
						}(i)
					}
				default:
					// No result yet
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Wait for Reconstruction
	reconstructed := make(map[int]bool)
	timeout = time.After(5 * time.Second)

	for len(reconstructed) < n {
		select {
		case <-timeout:
			t.Fatalf("Timeout waiting for reconstruction. Completed: %v", reconstructed)
		default:
			for i := 1; i <= n; i++ {
				if reconstructed[i] {
					continue
				}
				select {
				case res := <-results[i]:
					if res.InstanceID == instanceID && res.Type == "RECONSTRUCTED" {
						if res.Secret.Cmp(secret) != 0 {
							t.Errorf("Node %d reconstructed wrong secret: %v (expected %v)", i, res.Secret, secret)
						} else {
							t.Logf("Node %d reconstructed correct secret", i)
						}
						reconstructed[i] = true
					}
				default:
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestIVSS_Concurrent(t *testing.T) {
	n := 4
	f := 1
	servicesList, managers, results := setupIVSSAdvanced(t, n, f)
	defer func() {
		for _, m := range managers {
			if m != nil {
				m.Stop()
			}
		}
	}()

	// Run multiple instances concurrently
	numInstances := 5
	var wg sync.WaitGroup

	for k := 0; k < numInstances; k++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			dealerID := (idx % n) + 1
			secret := big.NewInt(int64(100 + idx))
			instanceID := "IVSS-CONC-" + string(rune('A'+idx))

			t.Logf("Starting instance %s (Dealer: %d, Secret: %v)", instanceID, dealerID, secret)

			err := servicesList[dealerID].StartSharing(instanceID, secret, managers[dealerID])
			if err != nil {
				t.Errorf("Failed to start sharing %s: %v", instanceID, err)
				return
			}
		}(k)
	}

	// Instead of per-instance waiting inside goroutine, let's have a central loop checking results

	expectedSecrets := make(map[string]*big.Int)
	for k := 0; k < numInstances; k++ {
		instanceID := "IVSS-CONC-" + string(rune('A'+k))
		expectedSecrets[instanceID] = big.NewInt(int64(100 + k))
	}

	// Track progress
	sharingState := make(map[string]map[int]bool) // instance -> node -> bool
	reconState := make(map[string]map[int]bool)   // instance -> node -> bool

	for k := 0; k < numInstances; k++ {
		id := "IVSS-CONC-" + string(rune('A'+k))
		sharingState[id] = make(map[int]bool)
		reconState[id] = make(map[int]bool)
	}

	timeout := time.After(20 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent instances")
			return
		case <-ticker.C:
			// Check all channels
			allDone := true
			for i := 1; i <= n; i++ {
				// Drain channel
			Loop:
				for {
					select {
					case res := <-results[i]:
						if res.Type == "SHARING_COMPLETE" {
							sharingState[res.InstanceID][i] = true
							// Trigger reconstruction
							go servicesList[i].StartReconstruction(res.InstanceID, managers[i])
						} else if res.Type == "RECONSTRUCTED" {
							reconState[res.InstanceID][i] = true
							expected := expectedSecrets[res.InstanceID]
							if res.Secret.Cmp(expected) != 0 {
								t.Errorf("Node %d Instance %s wrong secret %v != %v", i, res.InstanceID, res.Secret, expected)
							}
						}
					default:
						break Loop
					}
				}
			}

			// Check if everything is done
			for k := 0; k < numInstances; k++ {
				id := "IVSS-CONC-" + string(rune('A'+k))
				if len(reconState[id]) < n {
					allDone = false
					break
				}
			}

			if allDone {
				t.Log("All concurrent instances completed successfully")
				return
			}
		}
	}
}
