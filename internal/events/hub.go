// Package events is the in-process pub/sub hub that feeds SSE streams.
package events

import (
	"sync"
)

// Event is one message for a topic. ID is the activity ID when there is
// one. Name is the SSE event name.
type Event struct {
	ID   string
	Name string
	Data []byte
}

// Sub is one subscriber. C is closed when the subscriber falls too far
// behind or the hub drops it. The client then reconnects and replays.
type Sub struct {
	C     chan Event
	topic string
	once  sync.Once
}

func (s *Sub) close() { s.once.Do(func() { close(s.C) }) }

// Hub fans events out to subscribers by topic.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*Sub]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[string]map[*Sub]struct{}{}} }

// BoardTopic and UserTopic name the two kinds of stream.
func BoardTopic(id string) string { return "board:" + id }
func UserTopic(id string) string  { return "user:" + id }

// Subscribe adds a subscriber to topic.
func (h *Hub) Subscribe(topic string) *Sub {
	s := &Sub{C: make(chan Event, 256), topic: topic}
	h.mu.Lock()
	m := h.subs[topic]
	if m == nil {
		m = map[*Sub]struct{}{}
		h.subs[topic] = m
	}
	m[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes s and closes its channel.
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	if m := h.subs[s.topic]; m != nil {
		delete(m, s)
		if len(m) == 0 {
			delete(h.subs, s.topic)
		}
	}
	h.mu.Unlock()
	s.close()
}

// Publish sends ev to every subscriber of topic without blocking. A
// subscriber with a full buffer is dropped.
func (h *Hub) Publish(topic string, ev Event) {
	h.mu.RLock()
	var slow []*Sub
	for s := range h.subs[topic] {
		select {
		case s.C <- ev:
		default:
			slow = append(slow, s)
		}
	}
	h.mu.RUnlock()
	for _, s := range slow {
		h.Unsubscribe(s)
	}
}

// Count returns the number of subscribers, for metrics.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, m := range h.subs {
		n += len(m)
	}
	return n
}
