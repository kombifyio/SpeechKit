package stt

import "github.com/coder/websocket"

// IsWebSocketClose reports whether err is a WebSocket close, as opposed to a
// transport failure. Streaming providers treat the two differently: a close is
// the end of a session, a transport failure is worth surfacing.
func IsWebSocketClose(err error) bool {
	return websocket.CloseStatus(err) != -1
}
