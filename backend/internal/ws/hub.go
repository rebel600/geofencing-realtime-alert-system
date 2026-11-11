// Package ws implements the real-time alert broadcast over WebSockets.
//
// Design: one Hub goroutine owns the set of clients (no locks on the hot path).
// Each client has its own buffered send queue and writer goroutine, so a slow
// client cannot stall delivery to others; if its queue fills it is dropped
// and the browser reconnects. Publish never blocks the HTTP request.
package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second // must be < pongWait
	clientQueue    = 64
	publishBacklog = 1024
)

// Hub fans alert messages out to every connected client.
type Hub struct {
	register   chan *client
	unregister chan *client
	broadcast  chan []byte
	count      chan chan int
	done       chan struct{}
	upgrader   websocket.Upgrader
	log        *slog.Logger
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

// NewHub creates a hub. checkOrigin decides which browser origins may connect.
func NewHub(log *slog.Logger, checkOrigin func(r *http.Request) bool) *Hub {
	return &Hub{
		register:   make(chan *client),
		unregister: make(chan *client),
		broadcast:  make(chan []byte, publishBacklog),
		count:      make(chan chan int),
		done:       make(chan struct{}),
		upgrader:   websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 4096, CheckOrigin: checkOrigin},
		log:        log,
	}
}

// Run owns the client set; call it in its own goroutine. It returns when stop is closed.
func (h *Hub) Run(stop <-chan struct{}) {
	defer close(h.done)
	clients := map[*client]struct{}{}
	for {
		select {
		case c := <-h.register:
			clients[c] = struct{}{}
			h.log.Info("ws client connected", "clients", len(clients))
		case c := <-h.unregister:
			if _, ok := clients[c]; ok {
				delete(clients, c)
				close(c.send)
				h.log.Info("ws client disconnected", "clients", len(clients))
			}
		case msg := <-h.broadcast:
			for c := range clients {
				select {
				case c.send <- msg:
				default: // client is too slow; drop it, it will reconnect
					delete(clients, c)
					close(c.send)
					h.log.Warn("ws client dropped: send queue full")
				}
			}
		case reply := <-h.count:
			reply <- len(clients)
		case <-stop:
			for c := range clients {
				close(c.send)
			}
			return
		}
	}
}

// Publish queues a message for broadcast without blocking. Alerts are already
// persisted before Publish is called, so a drop here never loses data.
func (h *Hub) Publish(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		h.log.Error("ws marshal alert", "err", err)
		return
	}
	select {
	case h.broadcast <- b:
	default:
		h.log.Warn("ws broadcast backlog full; alert not pushed live (still stored)")
	}
}

// Clients returns the number of connected clients.
func (h *Hub) Clients() int {
	reply := make(chan int, 1)
	select {
	case h.count <- reply:
		return <-reply
	case <-h.done:
		return 0
	}
}

// ServeHTTP upgrades the request and attaches the connection to the hub.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an HTTP error
	}
	c := &client{conn: conn, send: make(chan []byte, clientQueue)}
	select {
	case h.register <- c:
	case <-h.done:
		conn.Close()
		return
	}
	go h.writePump(c)
	go h.readPump(c)
}

// readPump discards inbound messages but is required to process control
// frames (pong, close) and to notice when the peer goes away.
func (h *Hub) readPump(c *client) {
	defer func() {
		select {
		case h.unregister <- c:
		case <-h.done:
		}
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
