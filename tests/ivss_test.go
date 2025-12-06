package tests

import (
	"async-agreement-protocol-3/services"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// --- Stress Test Infrastructure ---

var (
	instanceResultsMu sync.Mutex
	instanceResults   = make(map[string]map[int]chan services.IVSSResult)
)

func registerInstanceListener(instanceID string, n int) {
	instanceResultsMu.Lock()
	defer instanceResultsMu.Unlock()
	instanceResults[instanceID] = make(map[int]chan services.IVSSResult)
	for i := 1; i <= n; i++ {
		instanceResults[instanceID][i] = make(chan services.IVSSResult, 100)
	}
}

func dispatchResult(nodeID int, res services.IVSSResult) {
	instanceResultsMu.Lock()
	defer instanceResultsMu.Unlock()
	if chans, ok := instanceResults[res.InstanceID]; ok {
		if ch, ok := chans[nodeID]; ok {
			ch <- res
		}
	}
}

func setupIVSSWithDemux(t *testing.T, n, f int) (*services.Network[services.IVSSMessage], []*services.IVSSService, []*services.ServiceManager[services.IVSSMessage, services.IVSSResult]) {
	network := services.NewNetwork[services.IVSSMessage]()
	managers := make([]*services.ServiceManager[services.IVSSMessage, services.IVSSResult], n+1)
	servicesList := make([]*services.IVSSService, n+1)

	for i := 1; i <= n; i++ {
		cp := services.NewCertificationProtocol()
		svc := services.NewIVSSService(i, n, f, cp, zerolog.Disabled)
		servicesList[i] = svc
		mgr := services.NewServiceManager[services.IVSSMessage, services.IVSSResult](svc, network)
		managers[i] = mgr
		network.Register(i, mgr.Inbox())
		mgr.Start()

		go func(id int, m *services.ServiceManager[services.IVSSMessage, services.IVSSResult]) {
			for res := range m.Result() {
				dispatchResult(id, res)
			}
		}(i, mgr)
	}
	return network, servicesList, managers
}

// --- Tests ---

func TestIVSS_NormalExecution(t *testing.T) {
	n := 4
	f := 1
	_, servicesList, managers := setupIVSSWithDemux(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	secretVal := int64(42)
	secret := big.NewInt(secretVal)
	instanceID := "test-ivss-1"
	registerInstanceListener(instanceID, n)

	// Start Sharing
	servicesList[1].StartSharing(instanceID, secret, managers[1])

	// Wait for Sharing Complete
	results := instanceResults[instanceID]
	waitForSharing(t, n, results, instanceID)

	t.Log("All nodes completed sharing. Starting Reconstruction...")

	// Start Reconstruction
	for i := 1; i <= n; i++ {
		servicesList[i].StartReconstruction(instanceID, managers[i])
	}

	// Wait for Reconstruction Complete
	waitForReconstruction(t, n, results, instanceID, secret)
	t.Log("IVSS Protocol Test Passed Successfully")
}

func TestIVSS_SilentNode(t *testing.T) {
	n := 4
	f := 1
	_, servicesList, managers := setupIVSSWithDemux(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	// Stop node 4 to simulate silence
	managers[4].Stop()

	secretVal := int64(99)
	secret := big.NewInt(secretVal)
	instanceID := "test-ivss-silent-1"
	registerInstanceListener(instanceID, n)

	// Start Sharing
	servicesList[1].StartSharing(instanceID, secret, managers[1])

	// Wait for Sharing Complete (expecting n-1 nodes)
	time.Sleep(2 * time.Second)

	results := instanceResults[instanceID]
	count := 0
	for _, ch := range results {
		select {
		case res := <-ch:
			if res.Type == "SHARING_COMPLETE" {
				count++
			}
		default:
		}
	}

	if count < n-f {
		t.Errorf("Expected at least %d nodes to complete sharing, got %d", n-f, count)
	}
	t.Logf("Silent Node Test: %d nodes completed sharing", count)
}

func TestIVSS_Stress_Concurrent(t *testing.T) {
	n := 4
	f := 1
	_, servicesList, managers := setupIVSSWithDemux(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	numInstances := 20 // Run 20 concurrent instances
	var wg sync.WaitGroup

	for k := 0; k < numInstances; k++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			instanceID := fmt.Sprintf("stress-ivss-%d", idx)
			t.Logf("Starting instance %s", instanceID)
			registerInstanceListener(instanceID, n)

			secret := big.NewInt(int64(1000 + idx))
			dealerID := (idx % n) + 1

			// Start Sharing
			servicesList[dealerID].StartSharing(instanceID, secret, managers[dealerID])

			if !waitForSharingWithDemuxTimeout(t, n, instanceID, 30*time.Second) {
				t.Errorf("Sharing timed out for %s", instanceID)
				return
			}

			// Start Reconstruction
			for i := 1; i <= n; i++ {
				servicesList[i].StartReconstruction(instanceID, managers[i])
			}

			if !waitForReconstructionWithDemuxTimeout(t, n, instanceID, secret, 30*time.Second) {
				t.Errorf("Reconstruction timed out for %s", instanceID)
				return
			}
			t.Logf("Finished instance %s", instanceID)
		}(k)
	}
	wg.Wait()
}

// --- Wait Helpers ---

func allNodes(n int) []int {
	res := make([]int, n)
	for i := 0; i < n; i++ {
		res[i] = i + 1
	}
	return res
}

func waitForSharing(t *testing.T, n int, results map[int]chan services.IVSSResult, instanceID string) {
	waitForSharingSubset(t, allNodes(n), results, instanceID)
}

func waitForSharingSubset(t *testing.T, nodes []int, results map[int]chan services.IVSSResult, instanceID string) {
	var wg sync.WaitGroup
	wg.Add(len(nodes))

	for _, id := range nodes {
		go func(nodeID int) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case res := <-results[nodeID]:
					if res.InstanceID == instanceID && res.Type == "SHARING_COMPLETE" {
						return
					}
				case <-timeout:
					t.Errorf("Node %d timed out waiting for SHARING_COMPLETE for %s", nodeID, instanceID)
					return
				}
			}
		}(id)
	}
	wg.Wait()
}

func waitForReconstruction(t *testing.T, n int, results map[int]chan services.IVSSResult, instanceID string, secret *big.Int) {
	waitForReconstructionSubset(t, allNodes(n), results, instanceID, secret)
}

func waitForReconstructionSubset(t *testing.T, nodes []int, results map[int]chan services.IVSSResult, instanceID string, secret *big.Int) {
	var wg sync.WaitGroup
	wg.Add(len(nodes))

	for _, id := range nodes {
		go func(nodeID int) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case res := <-results[nodeID]:
					if res.InstanceID == instanceID && res.Type == "RECONSTRUCTED" {
						if res.Secret.Cmp(secret) != 0 {
							t.Errorf("Node %d reconstructed wrong secret", nodeID)
						}
						return
					}
				case <-timeout:
					t.Errorf("Node %d timed out waiting for RECONSTRUCTED for %s", nodeID, instanceID)
					return
				}
			}
		}(id)
	}
	wg.Wait()
}

func waitForSharingWithDemuxTimeout(t *testing.T, n int, instanceID string, timeoutDur time.Duration) bool {
	instanceResultsMu.Lock()
	chans := instanceResults[instanceID]
	instanceResultsMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(n)
	success := true
	var successMu sync.Mutex

	for i := 1; i <= n; i++ {
		go func(id int) {
			defer wg.Done()
			select {
			case res := <-chans[id]:
				if res.Type != "SHARING_COMPLETE" {
					// Ignore other types if any
				}
			case <-time.After(timeoutDur):
				successMu.Lock()
				success = false
				successMu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return success
}

func waitForReconstructionWithDemuxTimeout(t *testing.T, n int, instanceID string, expectedSecret *big.Int, timeoutDur time.Duration) bool {
	instanceResultsMu.Lock()
	chans := instanceResults[instanceID]
	instanceResultsMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(n)
	success := true
	var successMu sync.Mutex

	for i := 1; i <= n; i++ {
		go func(id int) {
			defer wg.Done()
			select {
			case res := <-chans[id]:
				if res.Type == "RECONSTRUCTED" {
					if res.Secret.Cmp(expectedSecret) != 0 {
						t.Errorf("Node %d reconstructed wrong secret", id)
						successMu.Lock()
						success = false
						successMu.Unlock()
					}
				}
			case <-time.After(timeoutDur):
				successMu.Lock()
				success = false
				successMu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return success
}
