// Package relay implements a minimal WebSocket pub/sub relay compatible with
// Trystero's self-hosted "createTopicStrategy" client protocol
// (https://github.com/dmotz/trystero#write-your-own-strategy):
//
//	Client -> Server: {"type": "subscribe" | "unsubscribe" | "publish", "topic": "...", "payload": ...}
//	Server -> Client: {"topic": "...", "payload": ...}   (broadcast to all subscribers of that topic)
//
// It carries no knowledge of WebRTC, rooms, or peers — it just relays JSON
// blobs between browsers so they can exchange WebRTC session descriptions
// (SDP) and find each other. All call audio/video stays peer-to-peer.
package relay

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 1 << 16 // 64KB, plenty for SDP/ICE payloads
)

type inboundMessage struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type outboundMessage struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	mu     sync.Mutex
	topics map[string]bool
}

// Hub tracks topic subscriptions and fans out published messages.
type Hub struct {
	mu       sync.RWMutex
	topics   map[string]map[*client]bool
	upgrader websocket.Upgrader
}

// NewHub creates an empty relay hub.
func NewHub() *Hub {
	return &Hub{
		topics: make(map[string]map[*client]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// Signaling only ever carries opaque SDP/ICE blobs and rooms are
			// namespaced by an app-chosen topic string, so we don't need
			// origin-based restriction here.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// ServeWS upgrades an HTTP request to a WebSocket and services it until the
// connection closes.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("relay: upgrade failed: %v", err)
		return
	}

	c := &client{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 32),
		topics: make(map[string]bool),
	}

	go c.writePump()
	c.readPump()
}

func (c *client) readPump() {
	defer func() {
		c.hub.unsubscribeAll(c)
		close(c.send)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMsgSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg inboundMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // ignore malformed frames rather than dropping the connection
		}

		switch msg.Type {
		case "subscribe":
			c.hub.subscribe(c, msg.Topic)
		case "unsubscribe":
			c.hub.unsubscribe(c, msg.Topic)
		case "publish":
			c.hub.publish(msg.Topic, msg.Payload)
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case data, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) subscribe(c *client, topic string) {
	if topic == "" {
		return
	}
	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*client]bool)
	}
	h.topics[topic][c] = true
	h.mu.Unlock()

	c.mu.Lock()
	c.topics[topic] = true
	c.mu.Unlock()
}

func (h *Hub) unsubscribe(c *client, topic string) {
	h.mu.Lock()
	if subs, ok := h.topics[topic]; ok {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}
	h.mu.Unlock()

	c.mu.Lock()
	delete(c.topics, topic)
	c.mu.Unlock()
}

func (h *Hub) unsubscribeAll(c *client) {
	c.mu.Lock()
	topics := make([]string, 0, len(c.topics))
	for t := range c.topics {
		topics = append(topics, t)
	}
	c.mu.Unlock()

	for _, t := range topics {
		h.unsubscribe(c, t)
	}
}

func (h *Hub) publish(topic string, payload json.RawMessage) {
	out := outboundMessage{Topic: topic, Payload: payload}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}

	h.mu.RLock()
	subs := h.topics[topic]
	targets := make([]*client, 0, len(subs))
	for c := range subs {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// Slow/stuck client — drop the message rather than blocking the
			// whole hub. Signaling messages are small and re-sent by
			// Trystero's announce/reconnect logic if needed.
		}
	}
}
