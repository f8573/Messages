package realtime

import "testing"

func TestSendJSONAfterUnregisterDoesNotPanic(t *testing.T) {
	h := &Handler{
		clients: map[string]map[*client]struct{}{},
	}
	c := &client{
		userID: "user-1",
		send:   make(chan []byte, 1),
	}

	h.register(c)
	h.unregister(c)

	h.sendJSON(c, "event", map[string]any{"ok": true})
}
