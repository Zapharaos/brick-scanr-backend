package jobs

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// WebSocket upgrader
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Allow all origins for now (configure based on CORS settings in production)
			return true
		},
	}

	// Active WebSocket connections
	wsConnections = make(map[string]*websocket.Conn)
	wsLock        sync.RWMutex
)

// WSMessage represents a WebSocket message
// @Description WebSocket message for real-time updates
type WSMessage struct {
	Type    string      `json:"type" example:"status"`
	JobID   string      `json:"job_id" example:"abc123"`
	Payload interface{} `json:"payload"`
}

// WSStatusUpdate represents a status update message
// @Description Status update via WebSocket
type WSStatusUpdate struct {
	Status  string `json:"status" example:"started"`
	Message string `json:"message" example:"Fetching inventory..."`
}

// WSProgressUpdate represents a progress update message
// @Description Progress update via WebSocket
type WSProgressUpdate struct {
	Current int    `json:"current" example:"50"`
	Total   int    `json:"total" example:"193"`
	Message string `json:"message" example:"Loaded 50/193 prices"`
}

// HandleWebSocket godoc
//
//	@Summary		WebSocket connection for real-time updates
//	@Description	Opens a WebSocket connection to receive real-time updates for set details loading jobs
//	@Tags			WebSocket
//	@Produce		json
//	@Param			job_id	query	string	false	"Optional job ID to receive updates for a specific job"
//	@Success		101		{object}	WSMessage	"WebSocket connection established"
//	@Router			/api/v1/ws [get]
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("Failed to upgrade to WebSocket", zap.Error(err))
		return
	}

	// Generate connection ID
	connID := chi.URLParam(r, "id")
	if connID == "" {
		connID = fmt.Sprintf("%p", conn)
	}

	// Store connection
	wsLock.Lock()
	wsConnections[connID] = conn
	wsLock.Unlock()

	zap.L().Info("WebSocket connection established",
		zap.String("conn_id", connID),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Send welcome message
	welcomeMsg := WSMessage{
		Type: "connected",
		Payload: map[string]string{
			"message":       "WebSocket connected",
			"connection_id": connID,
		},
	}
	if err := conn.WriteJSON(welcomeMsg); err != nil {
		zap.L().Error("Failed to send welcome message", zap.Error(err))
	}

	// Handle incoming messages (ping/pong, close, etc.)
	go func() {
		defer func() {
			wsLock.Lock()
			delete(wsConnections, connID)
			wsLock.Unlock()
			conn.Close()
			zap.L().Info("WebSocket connection closed", zap.String("conn_id", connID))
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					zap.L().Error("WebSocket error", zap.Error(err))
				}
				break
			}
		}
	}()
}

// BroadcastToWebSocket sends a message to a specific WebSocket connection
func BroadcastToWebSocket(connID string, message WSMessage) error {
	wsLock.RLock()
	conn, exists := wsConnections[connID]
	wsLock.RUnlock()

	if !exists {
		return fmt.Errorf("connection not found: %s", connID)
	}

	if err := conn.WriteJSON(message); err != nil {
		zap.L().Error("Failed to send WebSocket message",
			zap.String("conn_id", connID),
			zap.Error(err),
		)
		return err
	}

	return nil
}

// BroadcastToAll sends a message to all connected WebSocket clients
func BroadcastToAll(message WSMessage) {
	wsLock.RLock()
	defer wsLock.RUnlock()

	for connID, conn := range wsConnections {
		if err := conn.WriteJSON(message); err != nil {
			zap.L().Error("Failed to broadcast to WebSocket",
				zap.String("conn_id", connID),
				zap.Error(err),
			)
		}
	}
}

// GetActiveConnections returns the count of active WebSocket connections
func GetActiveConnections() int {
	wsLock.RLock()
	defer wsLock.RUnlock()
	return len(wsConnections)
}

// SendWSStatus sends a status update via WebSocket
func SendWSStatus(connID, jobID, status, message string) {
	msg := WSMessage{
		Type:  "status",
		JobID: jobID,
		Payload: WSStatusUpdate{
			Status:  status,
			Message: message,
		},
	}
	_ = BroadcastToWebSocket(connID, msg)
}

// SendWSProgress sends a progress update via WebSocket
func SendWSProgress(connID, jobID string, current, total int, message string) {
	msg := WSMessage{
		Type:  "progress",
		JobID: jobID,
		Payload: WSProgressUpdate{
			Current: current,
			Total:   total,
			Message: message,
		},
	}
	_ = BroadcastToWebSocket(connID, msg)
}

// SendWSComplete sends a completion message via WebSocket
func SendWSComplete(connID, jobID string, data interface{}) {
	msg := WSMessage{
		Type:    "complete",
		JobID:   jobID,
		Payload: data,
	}
	_ = BroadcastToWebSocket(connID, msg)
}

// SendWSError sends an error message via WebSocket
func SendWSError(connID, jobID, errorMsg string) {
	msg := WSMessage{
		Type:  "error",
		JobID: jobID,
		Payload: map[string]string{
			"error": errorMsg,
		},
	}
	_ = BroadcastToWebSocket(connID, msg)
}

// WebSocketHandler interface for sending updates (to avoid circular dependency)
type WebSocketHandler interface {
	SendStatus(connID, jobID, status, message string)
	SendProgress(connID, jobID string, current, total int, message string)
	SendComplete(connID, jobID string, data interface{})
	SendError(connID, jobID, errorMsg string)
}

// WsHandlerAdapter adapts the WebSocket handler functions to the interface
type WsHandlerAdapter struct{}

func (WsHandlerAdapter) SendStatus(connID, jobID, status, message string) {
	SendWSStatus(connID, jobID, status, message)
}

func (WsHandlerAdapter) SendProgress(connID, jobID string, current, total int, message string) {
	SendWSProgress(connID, jobID, current, total, message)
}

func (WsHandlerAdapter) SendComplete(connID, jobID string, data interface{}) {
	SendWSComplete(connID, jobID, data)
}

func (WsHandlerAdapter) SendError(connID, jobID, errorMsg string) {
	SendWSError(connID, jobID, errorMsg)
}
