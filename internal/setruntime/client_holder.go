package setruntime

import (
	"sync"

	"github.com/google/uuid"
)

// clientHolder holds clients, it to be shared between checkout and checkout type,
// so that the clients can be updated on both sides
type clientHolder struct {
	clients     map[uuid.UUID]Client
	clientMutex sync.RWMutex
	needsMutex  bool
}

// newClientHolder creates a new client holder
// child is the child client holder, if any changes are made to the parent, the changes will be propagated to the child
// needsMutex indicates whether the client holder needs a mutex
func newClientHolder(needsMutex bool) *clientHolder {
	return &clientHolder{
		clients:     make(map[uuid.UUID]Client),
		clientMutex: sync.RWMutex{},
		needsMutex:  needsMutex,
	}
}

// broadcastPacket sends a packet to all clients
func (e *clientHolder) broadcastPacket(p Packet) {
	if e.needsMutex {
		e.clientMutex.RLock()
		defer e.clientMutex.RUnlock()
	}

	for _, c := range e.clients {
		c.SendPacket(p)
	}
}

// registerClient registers a client in the clients list
func (e *clientHolder) registerClient(cli Client) {
	if e.needsMutex {
		e.clientMutex.Lock()
		defer e.clientMutex.Unlock()
	}

	e.clients[cli.ID()] = cli
}

// unregisterClient unregisters a client from the clients list
func (e *clientHolder) unregisterClient(clientId uuid.UUID) {
	if e.needsMutex {
		e.clientMutex.Lock()
		defer e.clientMutex.Unlock()
	}

	delete(e.clients, clientId)
}
