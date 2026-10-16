package main

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func (e Event) JSON() []byte {
	data, _ := json.Marshal(e)
	return data
}

type EventBroker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

func NewEventBroker() *EventBroker {
	return &EventBroker{subscribers: make(map[string][]chan Event)}
}

func (b *EventBroker) Subscribe(taskID string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 32)
	b.subscribers[taskID] = append(b.subscribers[taskID], ch)
	return ch
}

func (b *EventBroker) Unsubscribe(taskID string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[taskID]
	for i, s := range subs {
		if s == ch {
			b.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

func (b *EventBroker) Broadcast(taskID string, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers[taskID] {
		select {
		case ch <- event:
		default:
		}
	}
}
