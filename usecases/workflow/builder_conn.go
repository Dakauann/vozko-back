package workflow_usecase

import "time"

// BuilderConn is the minimal WebSocket transport the AI builder session needs.
// Depending on this interface (instead of *websocket.Conn directly) lets the
// delivery layer wrap the connection to record the conversation thread WITHOUT
// touching any agent logic. gorilla/websocket's *Conn satisfies it as-is.
type BuilderConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v interface{}) error
	SetWriteDeadline(t time.Time) error
}
