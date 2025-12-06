package services

type Service[TMsg any, TRes any] interface {
	OnMessage(msg TMsg, ctx ServiceContext[TMsg, TRes])
}

type ServiceContext[TMsg any, TRes any] interface {
	Broadcast(msg TMsg)
	// IMPORTANT: this is crucial thing that it is always used in OnMessage of a service
	// and should not be used in any goroutine becasuse here we do not synchronize access to awaitingMsgs
	SendResult(res TRes)
}

type ServiceManager[TMsg any, TRes any] struct {
	service      Service[TMsg, TRes]
	inbox        chan TMsg // For incoming messages that need to be processed
	outbox       chan TRes // For outgoing messages/results
	awaitingMsgs []TRes
	network      *Network[TMsg]
	stop         chan struct{}
}

func NewServiceManager[TMsg any, TRes any](service Service[TMsg, TRes], network *Network[TMsg]) *ServiceManager[TMsg, TRes] {
	return &ServiceManager[TMsg, TRes]{
		service:      service,
		inbox:        make(chan TMsg, 1000),
		outbox:       make(chan TRes, 1000),
		awaitingMsgs: make([]TRes, 0),
		network:      network,
		stop:         make(chan struct{}),
	}
}

func (sm *ServiceManager[TMsg, TRes]) Start() {
	go sm.loop()
}

func (sm *ServiceManager[TMsg, TRes]) Stop() {
	select {
	case <-sm.stop:
		// Already closed
	default:
		close(sm.stop)
	}
}

func (sm *ServiceManager[TMsg, TRes]) Result() <-chan TRes {
	return sm.outbox
}

func (sm *ServiceManager[TMsg, TRes]) Inbox() chan TMsg {
	return sm.inbox
}

func (sm *ServiceManager[TMsg, TRes]) loop() {
	for {
		if len(sm.awaitingMsgs) > 0 {
			var nextMsg = sm.awaitingMsgs[0]
			select {
			case msg := <-sm.inbox:
				sm.service.OnMessage(msg, sm)
			case sm.outbox <- nextMsg:
				sm.awaitingMsgs = sm.awaitingMsgs[1:]
			case <-sm.stop:
				return
			}
			continue
		}

		select {
		case msg := <-sm.inbox:
			sm.service.OnMessage(msg, sm)
		case <-sm.stop:
			return
		}

	}
}

// Implement ServiceContext
func (sm *ServiceManager[TMsg, TRes]) Broadcast(msg TMsg) {
	sm.network.Broadcast(msg)
}

func (sm *ServiceManager[TMsg, TRes]) SendResult(res TRes) {
	// IMPORTANT: this is crucial thing that it is always used in OnMessage of a service
	// and should not be used in any goroutine becasuse here we do not synchronize access to awaitingMsgs
	sm.awaitingMsgs = append(sm.awaitingMsgs, res)
}
