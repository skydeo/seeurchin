package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Hub fans out poll events to connected SSE clients, keyed by poll ID.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{}
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: map[string]map[chan []byte]struct{}{}}
}

// Subscribe registers a listener for pollID and returns its channel plus a
// cancel func that unsubscribes and closes the channel.
func (h *Hub) Subscribe(pollID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	if h.subs[pollID] == nil {
		h.subs[pollID] = map[chan []byte]struct{}{}
	}
	h.subs[pollID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set, ok := h.subs[pollID]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, pollID)
				}
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// publish delivers data to every subscriber of pollID, dropping messages to
// listeners whose buffer is full (they'll catch up on their next refetch).
func (h *Hub) publish(pollID string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[pollID] {
		select {
		case ch <- data:
		default:
		}
	}
}

// broadcast notifies subscribers that the poll changed; clients refetch state.
func (s *Server) broadcast(pollID, eventType string) {
	data, _ := json.Marshal(map[string]string{"type": eventType})
	s.hub.publish(pollID, data)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, err := s.pollFromCode(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.hub.Subscribe(p.ID)
	defer cancel()

	_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = io.WriteString(w, "event: update\ndata: ")
			_, _ = w.Write(msg)
			_, _ = io.WriteString(w, "\n\n")
			flusher.Flush()
		case <-keepalive.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
