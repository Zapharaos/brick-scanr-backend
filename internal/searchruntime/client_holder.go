package searchruntime

import (
	"sync"

	"github.com/google/uuid"
)

// clientHolder holds clients using the searchruntime-local Client interface.
type clientHolder struct {
	clients     map[uuid.UUID]Client
	clientMutex sync.RWMutex
}

func newClientHolder() *clientHolder {
	return &clientHolder{clients: make(map[uuid.UUID]Client)}
}

func (h *clientHolder) broadcastPacket(p Packet) {
	h.clientMutex.RLock()
	defer h.clientMutex.RUnlock()
	for _, c := range h.clients {
		c.SendPacket(p)
	}
}

func (h *clientHolder) registerClient(c Client) {
	h.clientMutex.Lock()
	defer h.clientMutex.Unlock()
	h.clients[c.ID()] = c
}

func (h *clientHolder) unregisterClient(id uuid.UUID) {
	h.clientMutex.Lock()
	defer h.clientMutex.Unlock()
	delete(h.clients, id)
}
