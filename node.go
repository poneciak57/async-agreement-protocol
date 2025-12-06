package main

import (
	"async-agreement-protocol-3/services"

	"github.com/rs/zerolog"
)

// Node represents a node in the network running the ABA protocol
type Node struct {
	ID      int
	ABA     *services.ABAService
	Manager *services.ServiceManager[services.ABAMessage, int]
}

// NewNode creates a new Node instance
func NewNode(id, n, t, initialEstimate int, network *services.Network[services.ABAMessage], cp *services.CertificationProtocol, logLevel zerolog.Level) *Node {
	aba := services.NewABAService(id, n, t, initialEstimate, cp, logLevel)
	manager := services.NewServiceManager[services.ABAMessage, int](aba, network)

	return &Node{
		ID:      id,
		ABA:     aba,
		Manager: manager,
	}
}

// Start starts the node's service manager
func (n *Node) Start() {
	n.Manager.Start()
	n.ABA.Start(n.Manager)
}

// Result returns the channel where the final decision will be sent
func (n *Node) Result() <-chan int {
	return n.Manager.Result()
}

// Inbox returns the channel for incoming messages (used for network registration)
func (n *Node) Inbox() chan services.ABAMessage {
	return n.Manager.Inbox()
}
