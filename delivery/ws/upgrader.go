package ws

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// wsUpgrader is the shared websocket upgrader for every ws endpoint (call session,
// conversation hub, workflow simulator/builder).
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}
