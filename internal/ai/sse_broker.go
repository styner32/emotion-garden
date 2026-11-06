package ai

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type SSEBroker struct {
	mu      sync.Mutex
	subs    map[uuid.UUID]map[chan struct{}]struct{}
	cancels map[uuid.UUID]context.CancelFunc
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		subs:    make(map[uuid.UUID]map[chan struct{}]struct{}),
		cancels: make(map[uuid.UUID]context.CancelFunc),
	}
}

func (b *SSEBroker) Subscribe(turnID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subs[turnID] == nil {
		b.subs[turnID] = make(map[chan struct{}]struct{})
	}
	b.subs[turnID][ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		if m, ok := b.subs[turnID]; ok {
			delete(m, ch)
			if len(m) == 0 {
				delete(b.subs, turnID)
			}
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func (b *SSEBroker) Notify(turnID uuid.UUID) {
	b.mu.Lock()
	m := b.subs[turnID]
	for ch := range m {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	b.mu.Unlock()
}

func (b *SSEBroker) RegisterRunCancel(runID uuid.UUID, cancel context.CancelFunc) {
	b.mu.Lock()
	b.cancels[runID] = cancel
	b.mu.Unlock()
}

func (b *SSEBroker) UnregisterRunCancel(runID uuid.UUID) {
	b.mu.Lock()
	delete(b.cancels, runID)
	b.mu.Unlock()
}

func (b *SSEBroker) CancelRun(runID uuid.UUID) {
	b.mu.Lock()
	fn, ok := b.cancels[runID]
	b.mu.Unlock()
	if ok && fn != nil {
		fn()
	}
}
