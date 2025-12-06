package tests

import (
	"async-agreement-protocol-3/services"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func setupICCAdvanced(t *testing.T, n, f, round int) ([]*services.ICCService, []*services.ServiceManager[services.ICCMessage, services.ICCResult], map[int]chan services.ICCResult) {
	network := services.NewNetwork[services.ICCMessage]()
	managers := make([]*services.ServiceManager[services.ICCMessage, services.ICCResult], n+1)
	servicesList := make([]*services.ICCService, n+1)
	results := make(map[int]chan services.ICCResult)

	for i := 1; i <= n; i++ {
		cp := services.NewCertificationProtocol()
		// Use InfoLevel to reduce log noise in large tests
		svc := services.NewICCService(i, n, f, round, cp, zerolog.InfoLevel)
		servicesList[i] = svc

		mgr := services.NewServiceManager[services.ICCMessage, services.ICCResult](svc, network)
		managers[i] = mgr

		network.Register(i, mgr.Inbox())
		results[i] = make(chan services.ICCResult, 100)

		mgr.Start()

		go func(id int, m *services.ServiceManager[services.ICCMessage, services.ICCResult]) {
			for res := range m.Result() {
				results[id] <- res
			}
		}(i, mgr)
	}

	return servicesList, managers, results
}

func TestICC_LargeCluster(t *testing.T) {
	// N=7, T=2
	n := 7
	f := 2
	servicesList, managers, results := setupICCAdvanced(t, n, f, 1)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	t.Log("Starting ICC Protocol (Large Cluster) on all nodes...")
	for i := 1; i <= n; i++ {
		go servicesList[i].Start(managers[i])
	}

	timeout := time.After(20 * time.Second)
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

	firstCoin := coins[1]
	for i := 2; i <= n; i++ {
		if coins[i] != firstCoin {
			t.Fatalf("Disagreement! Node 1: %d, Node %d: %d", firstCoin, i, coins[i])
		}
	}
	t.Logf("Agreement reached! Coin: %d", firstCoin)
}
