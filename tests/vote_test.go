package tests

import (
	"async-agreement-protocol-3/services"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func setupVote(t *testing.T, n, f, round int) (*services.Network[services.VoteMessage], []*services.VoteService, []*services.ServiceManager[services.VoteMessage, services.VoteResult]) {
	network := services.NewNetwork[services.VoteMessage]()
	managers := make([]*services.ServiceManager[services.VoteMessage, services.VoteResult], n+1)
	servicesList := make([]*services.VoteService, n+1)

	// log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	for i := 1; i <= n; i++ {
		svc := services.NewVoteService(i, n, f, zerolog.Disabled)
		servicesList[i] = svc
		mgr := services.NewServiceManager[services.VoteMessage, services.VoteResult](svc, network)
		managers[i] = mgr
		network.Register(i, mgr.Inbox())
		mgr.Start()
	}
	return network, servicesList, managers
}

func TestVote_Unanimous_1(t *testing.T) {
	n := 4
	f := 1
	round := 1
	_, servicesList, managers := setupVote(t, n, f, round)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	results := make(chan services.VoteResult, n)

	for i := 1; i <= n; i++ {
		go func(m *services.ServiceManager[services.VoteMessage, services.VoteResult]) {
			for res := range m.Result() {
				results <- res
			}
		}(managers[i])
	}

	// Start with input 1
	for i := 1; i <= n; i++ {
		go servicesList[i].StartRound(round, 1, managers[i])
	}

	// Wait for n results
	for i := 0; i < n; i++ {
		select {
		case res := <-results:
			if res.Value != 1 {
				t.Errorf("Expected value 1, got %d", res.Value)
			}
			if res.Conf != 2 {
				t.Errorf("Expected conf 2 (Strong), got %d", res.Conf)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for result")
		}
	}
}

func TestVote_Unanimous_0(t *testing.T) {
	n := 4
	f := 1
	round := 1
	_, servicesList, managers := setupVote(t, n, f, round)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	results := make(chan services.VoteResult, n)

	for i := 1; i <= n; i++ {
		go func(m *services.ServiceManager[services.VoteMessage, services.VoteResult]) {
			for res := range m.Result() {
				results <- res
			}
		}(managers[i])
	}

	// Start with input 0
	for i := 1; i <= n; i++ {
		go servicesList[i].StartRound(round, 0, managers[i])
	}

	// Wait for n results
	for i := 0; i < n; i++ {
		select {
		case res := <-results:
			if res.Value != 0 {
				t.Errorf("Expected value 0, got %d", res.Value)
			}
			if res.Conf != 2 {
				t.Errorf("Expected conf 2 (Strong), got %d", res.Conf)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for result")
		}
	}
}

func TestVote_Split_3vs1(t *testing.T) {
	n := 4
	f := 1
	round := 1
	_, servicesList, managers := setupVote(t, n, f, round)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	results := make(chan services.VoteResult, n)

	for i := 1; i <= n; i++ {
		go func(m *services.ServiceManager[services.VoteMessage, services.VoteResult]) {
			for res := range m.Result() {
				results <- res
			}
		}(managers[i])
	}

	// 3 nodes vote 1, 1 node votes 0
	// Majority should be 1
	for i := 1; i <= 3; i++ {
		go servicesList[i].StartRound(round, 1, managers[i])
	}
	go servicesList[4].StartRound(round, 0, managers[4])

	// Wait for n results
	for i := 0; i < n; i++ {
		select {
		case res := <-results:
			if res.Value != 1 {
				t.Errorf("Expected value 1, got %d", res.Value)
			}
			// Should still be strong majority because everyone will eventually see the majority
			if res.Conf != 2 {
				t.Errorf("Expected conf 2 (Strong), got %d", res.Conf)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for result")
		}
	}
}
