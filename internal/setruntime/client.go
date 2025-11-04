package setruntime

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type Client interface {
	ID() uuid.UUID
	LastActivity() time.Time
	Send(message string)
	SendPacket(packet Packet)
	close()
}

const (
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Buffer size for the send channel to prevent blocking while allowing some buffering
	sendChannelBufferSize = 64

	// Maximum consecutive send failures before considering the client disconnected
	maxConsecutiveSendFailures = 10
)

type clientMessage struct {
	client *client
	data   []byte
}

type client struct {
	userId uuid.UUID

	send chan []byte

	rs *RuntimeSet

	conn *websocket.Conn

	done chan struct{}

	lastActivity            time.Time
	consecutiveSendFailures int
	mutex                   sync.RWMutex
}

func NewClient(rs *RuntimeSet, conn *websocket.Conn, userId uuid.UUID) Client {
	cli := &client{
		send:         make(chan []byte, sendChannelBufferSize),
		done:         make(chan struct{}),
		conn:         conn,
		rs:           rs,
		userId:       userId,
		lastActivity: time.Now(),
		mutex:        sync.RWMutex{},
	}

	// Register the client with the runtime set
	rs.Register() <- cli

	// Start the read and write polling
	go cli.startReadPolling()
	go cli.startWritePolling(pingPeriod)

	return cli
}

func (c *client) startWritePolling(pingPeriod time.Duration) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.SetReadDeadline(time.Now().Add(time.Second)) // Set a read deadline to force the read polling to stop
	}()

	for {
		select {
		case message := <-c.send:
			if message == nil {
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "closing connection"))
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				zap.L().Error("Error writing message", zap.Error(err))
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				zap.L().Error("Error writing ping message", zap.Error(err))
				return
			}
		case <-c.done:
			return
		}
	}
}

// startReadPolling starts a goroutine that reads messages from the client
func (c *client) startReadPolling() {
	defer func() {
		if err := c.conn.Close(); err != nil {
			zap.L().Error("Error closing connection", zap.Error(err))
		}
		c.rs.Unregister() <- c.ID()
		select {
		case c.done <- struct{}{}:
		default:
		}
		c.close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				return
			}

			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				zap.L().Error("Error reading message IsUnexpectedCloseError", zap.Error(err))
				return
			}
			zap.L().Error("Error reading message", zap.Error(err))
			return
		}

		c.mutex.Lock()
		c.lastActivity = time.Now()
		c.mutex.Unlock()

		c.rs.Receive() <- clientMessage{
			client: c,
			data:   message,
		}
	}
}

// ID returns the client's ID
func (c *client) ID() uuid.UUID {
	return c.userId
}

// LastActivity returns the last time the client was active
func (c *client) LastActivity() time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.lastActivity
}

// Send sends a message to the client
func (c *client) Send(message string) {
	data := []byte(message)

	// Non-blocking send to prevent blocking the entire application
	select {
	case c.send <- data:
		// Message sent successfully - reset failure counter
		c.mutex.Lock()
		c.consecutiveSendFailures = 0
		c.mutex.Unlock()
	default:
		// Channel is blocked, track failures
		c.mutex.Lock()
		c.consecutiveSendFailures++
		failures := c.consecutiveSendFailures
		c.mutex.Unlock()

		// Log and check if we should disconnect
		zap.L().Warn("Client send channel blocked, dropping message",
			zap.String("client_id", c.userId.String()),
			zap.Int("consecutive_failures", failures))

		// If too many consecutive failures, the client is likely disconnected
		if failures >= maxConsecutiveSendFailures {
			zap.L().Warn("Too many consecutive send failures, closing client connection",
				zap.String("client_id", c.userId.String()),
				zap.Int("failures", failures))
			c.close()
		}
	}
}

// SendPacket sends a packet to the client
func (c *client) SendPacket(packet Packet) {
	data, err := packet.ToJSON()
	if err != nil {
		zap.L().Error("Error marshalling packet", zap.Error(err))
		return
	}

	// Non-blocking send to prevent blocking the entire application
	// If the send channel is full/blocked, we drop the message and log it
	select {
	case c.send <- data:
		// Message sent successfully - reset failure counter
		c.mutex.Lock()
		c.consecutiveSendFailures = 0
		c.mutex.Unlock()
	default:
		// Channel is blocked, track failures
		c.mutex.Lock()
		c.consecutiveSendFailures++
		failures := c.consecutiveSendFailures
		c.mutex.Unlock()

		// Log and check if we should disconnect
		zap.L().Warn("Client send channel blocked, dropping packet",
			zap.String("client_id", c.userId.String()),
			zap.Int("consecutive_failures", failures))

		// If too many consecutive failures, the client is likely disconnected
		if failures >= maxConsecutiveSendFailures {
			zap.L().Warn("Too many consecutive send failures, closing client connection",
				zap.String("client_id", c.userId.String()),
				zap.Int("failures", failures))
			c.close()
		}
	}
}

// close closes the client's connection, without sending or waiting for a close message
func (c *client) close() {
	select {
	case c.send <- nil:
	default:
	}

	time.AfterFunc(2*time.Second, func() {

		// Attempt to send a message to the done channel
		select {
		case c.done <- struct{}{}:
		default:
		}
	})
}
