package setruntime

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Client interface {
	ID() uuid.UUID
	LastActivity() time.Time
	SendPacket(packet Packet)
	close()
}

// clientConfig holds configuration for client behavior
type clientConfig struct {
	PingPeriod                 time.Duration
	SendChannelBufferSize      int
	MaxConsecutiveSendFailures int
}

func newClientConfig() clientConfig {
	// Set defaults
	viper.SetDefault("websocket.client.ping_period", 60)
	viper.SetDefault("websocket.client.send_channel_buffer_size", 64)
	viper.SetDefault("websocket.client.max_consecutive_send_failures", 10)

	return clientConfig{
		PingPeriod:                 viper.GetDuration("websocket.client.ping_period") * time.Second,
		SendChannelBufferSize:      viper.GetInt("websocket.client.send_channel_buffer_size"),
		MaxConsecutiveSendFailures: viper.GetInt("websocket.client.max_consecutive_send_failures"),
	}
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

	config clientConfig // store websocket config for this client
}

func NewClient(rs *RuntimeSet, conn *websocket.Conn, userId uuid.UUID) Client {
	config := newClientConfig()
	cli := &client{
		send:         make(chan []byte, config.SendChannelBufferSize),
		done:         make(chan struct{}),
		conn:         conn,
		rs:           rs,
		userId:       userId,
		lastActivity: time.Now(),
		mutex:        sync.RWMutex{},
		config:       config,
	}

	// Register the client with the runtime set
	rs.Register() <- cli

	// Start the read and write polling
	go cli.startWritePolling(config.PingPeriod)

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
		if failures >= c.config.MaxConsecutiveSendFailures {
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
