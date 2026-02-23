package searchruntime

import (
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/wsruntime"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client represents a connected WebSocket client.
type Client interface {
	ID() uuid.UUID
	LastActivity() time.Time
	SendPacket(packet Packet)
	close()
}

// client wraps wsruntime.BaseClient with the searchruntime-local interface.
type client struct {
	*wsruntime.BaseClient
}

// NewClient creates and registers a new WebSocket client for the given runtime.
func NewClient(rt *Runtime, conn *websocket.Conn, userID uuid.UUID) Client {
	onRegister := func(bc *wsruntime.BaseClient) {
		c := &client{BaseClient: bc}
		rt.register <- c
	}
	base := wsruntime.NewBaseClient(onRegister, rt.unregisterUUID(), conn, userID)
	return &client{BaseClient: base}
}

// SendPacket bridges the local Packet interface to wsruntime.Packet.
func (c *client) SendPacket(p Packet) {
	c.BaseClient.SendPacket(p)
}

// close delegates to Close on BaseClient.
func (c *client) close() {
	c.BaseClient.Close()
}
