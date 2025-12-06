package tests

import (
	"async-agreement-protocol-3/services"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// --- Helper Setup ---

func setupICC(t *testing.T, n, f int) ([]*services.ICCService, []*services.ServiceManager[services.ICCMessage, services.ICCResult], map[int]chan services.ICCResult) {
	// Create Network
	network := services.NewNetwork[services.ICCMessage]()

	// Create Nodes
	managers := make([]*services.ServiceManager[services.ICCMessage, services.ICCResult], n+1)
	servicesList := make([]*services.ICCService, n+1)
	results := make(map[int]chan services.ICCResult)

	for i := 1; i <= n; i++ {
		// Create dependencies
		cp := services.NewCertificationProtocol()
		svc := services.NewICCService(i, n, f, 1, cp, zerolog.DebugLevel)
		servicesList[i] = svc

		mgr := services.NewServiceManager[services.ICCMessage, services.ICCResult](svc, network)
		managers[i] = mgr

		network.Register(i, mgr.Inbox())
		results[i] = make(chan services.ICCResult, 100)

		// Start manager
		mgr.Start()

		// Collect results
		go func(id int, m *services.ServiceManager[services.ICCMessage, services.ICCResult]) {
			for res := range m.Result() {
				results[id] <- res
			}
		}(i, mgr)
	}

	return servicesList, managers, results
}

// --- Tests ---

func TestICC_NormalExecution(t *testing.T) {
	n := 4
	f := 1
	servicesList, managers, results := setupICC(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	t.Log("Starting ICC Protocol on all nodes...")

	// Start ICC on all nodes
	for i := 1; i <= n; i++ {
		go servicesList[i].Start(managers[i])
	}

	// Wait for results
	timeout := time.After(10 * time.Second)
	coins := make(map[int]int)

	for i := 1; i <= n; i++ {
		select {
		case res := <-results[i]:
			t.Logf("Node %d output coin: %d", i, res.Coin)
			coins[i] = res.Coin
		case <-timeout:
			t.Fatalf("Timeout waiting for node %d", i)
		}
	}

	// Verify Agreement
	firstCoin := coins[1]
	for i := 2; i <= n; i++ {
		if coins[i] != firstCoin {
			t.Fatalf("Disagreement! Node 1: %d, Node %d: %d", firstCoin, i, coins[i])
		}
	}

	t.Logf("Agreement reached! Coin: %d", firstCoin)
}

func TestICC_WithSilentNode(t *testing.T) {
	// N=4, T=1. Node 4 is silent (does not start).
	n := 4
	f := 1
	servicesList, managers, results := setupICC(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	t.Log("Starting ICC Protocol on nodes 1, 2, 3 (Node 4 silent)...")

	// Start ICC on correct nodes only
	for i := 1; i <= n-1; i++ {
		go servicesList[i].Start(managers[i])
	}

	// Wait for results for correct nodes
	timeout := time.After(10 * time.Second)
	coins := make(map[int]int)

	for i := 1; i <= n-1; i++ {
		select {
		case res := <-results[i]:
			t.Logf("Node %d output coin: %d", i, res.Coin)
			coins[i] = res.Coin
		case <-timeout:
			t.Fatalf("Timeout waiting for node %d", i)
		}
	}

	// Verify Agreement
	firstCoin := coins[1]
	for i := 2; i <= n-1; i++ {
		if coins[i] != firstCoin {
			t.Fatalf("Disagreement! Node 1: %d, Node %d: %d", firstCoin, i, coins[i])
		}
	}

	t.Logf("Agreement reached with silent node! Coin: %d", firstCoin)
}
