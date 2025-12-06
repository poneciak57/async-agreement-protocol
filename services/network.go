package services

import "sync"

type Network[TMsg any] struct {
	peers map[int]chan TMsg
	mu    sync.RWMutex
}

func NewNetwork[TMsg any]() *Network[TMsg] {
	return &Network[TMsg]{
		peers: make(map[int]chan TMsg),
	}
}

func (n *Network[TMsg]) Register(id int, ch chan TMsg) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers[id] = ch
}

func (n *Network[TMsg]) Broadcast(msg TMsg) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, ch := range n.peers {
		go func(c chan TMsg) {
			c <- msg
		}(ch)
	}
}
