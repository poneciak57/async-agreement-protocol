package services

import (
	"sync"
)

// CertificationProtocol maintains the set of Faulty Pairs (FP) and CoreInvocations.
type CertificationProtocol struct {
	fp              map[[2]int]bool // Set of faulty pairs {i, j}
	coreInvocations []string        // List of successful IVSS instance IDs
	mu              sync.RWMutex
}

func NewCertificationProtocol() *CertificationProtocol {
	return &CertificationProtocol{
		fp:              make(map[[2]int]bool),
		coreInvocations: make([]string, 0),
	}
}

// AddFaultyPair adds {i, j} to the set of faulty pairs.
// The pair is stored as {min(i,j), max(i,j)} to be unordered.
func (cp *CertificationProtocol) AddFaultyPair(i, j int) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if i > j {
		i, j = j, i
	}
	cp.fp[[2]int{i, j}] = true
}

// IsFaultyPair checks if {i, j} is in the set of faulty pairs.
func (cp *CertificationProtocol) IsFaultyPair(i, j int) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	if i > j {
		i, j = j, i
	}
	return cp.fp[[2]int{i, j}]
}

// AddCoreInvocation adds an instance ID to the history.
func (cp *CertificationProtocol) AddCoreInvocation(instanceID string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.coreInvocations = append(cp.coreInvocations, instanceID)
}

// GetCoreInvocations returns a copy of the history.
func (cp *CertificationProtocol) GetCoreInvocations() []string {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	// Return a copy to avoid races
	result := make([]string, len(cp.coreInvocations))
	copy(result, cp.coreInvocations)
	return result
}
