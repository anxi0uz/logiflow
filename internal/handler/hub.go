package handler

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Hub struct {
	mu          sync.RWMutex
	connections map[uuid.UUID][]*websocket.Conn
	trackers    map[uuid.UUID]context.CancelFunc
}

func NewHub() *Hub {
	return &Hub{
		connections: make(map[uuid.UUID][]*websocket.Conn),
		trackers:    make(map[uuid.UUID]context.CancelFunc),
	}
}

func (h *Hub) Register(orderID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[orderID] = append(h.connections[orderID], conn)
}

func (h *Hub) Unregister(orderID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.connections[orderID]
	for i, c := range conns {
		if c == conn {
			h.connections[orderID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
}

func (h *Hub) Broadcast(orderID uuid.UUID, msg any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, conn := range h.connections[orderID] {
		conn.WriteJSON(msg)
	}
}

func (h *Hub) StartTracker(orderID uuid.UUID, fn func(ctx context.Context)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.trackers[orderID]; exists {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.trackers[orderID] = cancel
	go fn(ctx)
}

func (h *Hub) StopTracker(orderID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cancel, exists := h.trackers[orderID]; exists {
		cancel()
		delete(h.trackers, orderID)
	}
}
