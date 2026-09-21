package workflow_usecase

import "time"

type BuilderConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v interface{}) error
	SetWriteDeadline(t time.Time) error
}
