package frontend

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// writeTimeout 是单次写入浏览器连接的上限。
//
// 有上限的原因：一个卡住的浏览器不应该拖死播报队列，超时就当这条消息没送到。
const writeTimeout = 5 * time.Second

// message 是下行消息的统一信封。
type message struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// clientMessage 是上行消息的统一信封。
type clientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// seqData 是上行回执携带的播报序号。
type seqData struct {
	Seq uint64 `json:"seq"`
}

// client 是一个已连接的前端。
type client struct {
	id   uint64
	conn *websocket.Conn

	mu sync.Mutex // 串行化写入，避免两条消息交错
}

// write 向该前端发送一条消息。
func (c *client) write(ctx context.Context, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	return c.conn.Write(writeCtx, websocket.MessageText, payload)
}

// hub 维护全部前端连接与播报回执的等待者。
type hub struct {
	mu      sync.RWMutex
	clients map[uint64]*client
	nextID  uint64

	// waiters 按播报序号登记「等前端播完」的通道。
	waiters map[uint64]chan struct{}
}

func newHub() *hub {
	return &hub{
		clients: make(map[uint64]*client),
		waiters: make(map[uint64]chan struct{}),
	}
}

// add 登记一个新连接。
func (h *hub) add(conn *websocket.Conn) *client {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	c := &client{id: h.nextID, conn: conn}
	h.clients[c.id] = c

	log.Sugar().Infof("前端已连接: id=%d 当前连接数=%d", c.id, len(h.clients))
	return c
}

// remove 注销连接。
func (h *hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[c.id] == c {
		delete(h.clients, c.id)
	}
	log.Sugar().Infof("前端已断开: id=%d 剩余连接数=%d", c.id, len(h.clients))
}

// count 返回当前连接数。
func (h *hub) count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}

// broadcast 向所有前端发送同一条消息，返回成功送达的连接数。
//
// 单个连接失败只记日志：一个前端掉线不该影响其它前端，也不该让播报失败。
func (h *hub) broadcast(ctx context.Context, msg message) int {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Sugar().Errorf("序列化下行消息失败: %v", err)
		return 0
	}

	h.mu.RLock()
	targets := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	sent := 0
	for _, c := range targets {
		if err := c.write(ctx, payload); err != nil {
			log.Sugar().Warnf("向下行前端 %d 发送失败: %v", c.id, err)
			continue
		}
		sent++
	}

	return sent
}

// registerWaiter 登记一个等待播报回执的通道。
func (h *hub) registerWaiter(seq uint64) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan struct{}, 1)
	h.waiters[seq] = ch

	return ch
}

// unregisterWaiter 注销等待者。
func (h *hub) unregisterWaiter(seq uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.waiters, seq)
}

// notifyPlayback 通知某个序号已经播完。
func (h *hub) notifyPlayback(seq uint64) {
	h.mu.RLock()
	ch, ok := h.waiters[seq]
	h.mu.RUnlock()

	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default: // 已经有回执在路上，不重复通知
	}
}
