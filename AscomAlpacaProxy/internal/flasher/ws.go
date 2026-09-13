package flasher

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"sv241pro-alpaca-proxy/internal/logger"

	"github.com/gorilla/websocket"
)

// wsHub/wsClient mirror internal/logstream's Hub/Client pattern (register/unregister, ping/pong
// keepalive) but broadcast JobStatus updates instead of raw log lines. Reimplemented here rather
// than reusing logstream's Hub directly: the payload shape (one JSON object per status change,
// not a continuous text firehose) and lifecycle (tied to a single flash job, not the whole
// process) are different enough that forcing reuse would add more coupling than it saves. At this
// scale (at most a handful of browser tabs watching one flash job at a time) a mutex-guarded map
// is simpler than a dedicated hub goroutine and channel set, and just as safe: every mutation of
// hub.clients or a close of a client's send channel happens under hub.mu, so broadcast can never
// send on a channel readPump's cleanup already closed (they're mutually exclusive critical
// sections, not just individually safe operations).
type wsHub struct {
	mu      sync.Mutex
	clients map[*wsClient]bool
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

var hub = &wsHub{clients: make(map[*wsClient]bool)}

// broadcast sends s to every connected /ws/flash client. A client whose send buffer is full
// (i.e. isn't reading fast enough) is dropped rather than allowed to block the flash goroutine
// calling this.
func broadcast(s JobStatus) {
	data, err := json.Marshal(s)
	if err != nil {
		logger.Error("flasher: failed to marshal status for broadcast: %v", err)
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for c := range hub.clients {
		select {
		case c.send <- data:
		default:
			close(c.send)
			delete(hub.clients, c)
		}
	}
}

// ServeWs handles WebSocket connections for live flash-progress updates. Sends the current status
// immediately on connect - so a client that connects mid-flash, or after the last job already
// finished, sees where things stand rather than only future changes - then streams every
// subsequent update until the connection closes.
func ServeWs(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("flasher: failed to upgrade to websocket: %v", err)
		return
	}

	client := &wsClient{conn: conn, send: make(chan []byte, 16)}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()

	if data, err := json.Marshal(CurrentStatus()); err == nil {
		client.send <- data
	}

	go client.writePump()
	client.readPump() // blocks until the connection closes
}

func (c *wsClient) readPump() {
	defer func() {
		hub.mu.Lock()
		if _, ok := hub.clients[c]; ok {
			delete(hub.clients, c)
			close(c.send)
		}
		hub.mu.Unlock()
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(50 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
