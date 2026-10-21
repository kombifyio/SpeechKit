package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// rpcError mirrors the JSON-RPC 2.0 error object. Code -32001 is the
// documented app-server backpressure signal ("Server overloaded; retry
// later").
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

const (
	rpcCodeOverloaded     = -32001
	rpcCodeMethodNotFound = -32601
)

// rpcID carries a JSON-RPC id verbatim: the spec allows numbers AND strings,
// and the app-server may use either, so the raw bytes are round-tripped
// untouched and used as the correlation key.
type rpcID struct{ raw json.RawMessage }

func (i rpcID) MarshalJSON() ([]byte, error) { return i.raw, nil }
func (i *rpcID) UnmarshalJSON(b []byte) error {
	i.raw = append([]byte(nil), b...)
	return nil
}
func (i rpcID) key() string { return string(i.raw) }

// rpcMessage is the loose wire shape. The codex app-server omits the
// "jsonrpc":"2.0" member on the wire (documented protocol quirk), so this
// client neither emits nor requires it.
type rpcMessage struct {
	ID     *rpcID          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// rpcRequestHandler serves an incoming server->client request. It runs on its
// own goroutine and MAY block (approval requests block until the user
// decides); returning sends the response.
type rpcRequestHandler func(ctx context.Context, method string, params json.RawMessage) (any, *rpcError)

// rpcNotifyHandler consumes an incoming notification. Must not block long.
type rpcNotifyHandler func(method string, params json.RawMessage)

// rpcConn is a minimal bidirectional JSON-RPC 2.0 client over newline-
// delimited JSON (the `codex app-server` stdio transport).
type rpcConn struct {
	w        io.Writer
	writeMu  sync.Mutex
	seq      atomic.Int64
	onNotify rpcNotifyHandler
	onReq    rpcRequestHandler

	mu      sync.Mutex
	pending map[string]chan rpcMessage
	closed  bool
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
}

func newRPCConn(r io.Reader, w io.Writer, onNotify rpcNotifyHandler, onReq rpcRequestHandler) *rpcConn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &rpcConn{
		w:        w,
		onNotify: onNotify,
		onReq:    onReq,
		pending:  map[string]chan rpcMessage{},
		done:     make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
	go c.readLoop(r)
	return c
}

var errConnClosed = errors.New("codex app-server connection closed")

// Call sends a request and waits for its response (or ctx/connection end).
func (c *rpcConn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := rpcID{raw: json.RawMessage(fmt.Sprintf("%d", c.seq.Add(1)))}
	ch := make(chan rpcMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errConnClosed
	}
	c.pending[id.key()] = ch
	c.mu.Unlock()

	if err := c.write(rpcMessage{ID: &id, Method: method, Params: mustRaw(params)}); err != nil {
		c.mu.Lock()
		delete(c.pending, id.key())
		c.mu.Unlock()
		return nil, err
	}
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id.key())
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, errConnClosed
	}
}

// Notify sends a fire-and-forget notification.
func (c *rpcConn) Notify(method string, params any) error {
	return c.write(rpcMessage{Method: method, Params: mustRaw(params)})
}

func (c *rpcConn) write(msg rpcMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

// Close tears the connection down and fails all pending calls.
func (c *rpcConn) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()
	c.cancel()
}

// Done reports connection end (EOF, decode failure, Close).
func (c *rpcConn) Done() <-chan struct{} { return c.done }

func (c *rpcConn) readLoop(r io.Reader) {
	defer c.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // tolerate non-JSON noise on the stream
		}
		switch {
		case msg.ID != nil && msg.Method != "":
			// Server -> client request (approvals). Serve without blocking
			// the read loop; the handler may wait minutes for a user click.
			id := *msg.ID
			go func(id rpcID, method string, params json.RawMessage) {
				result, rerr := c.onReq(c.ctx, method, params)
				resp := rpcMessage{ID: &id}
				if rerr != nil {
					resp.Error = rerr
				} else {
					resp.Result = mustRaw(result)
				}
				_ = c.write(resp)
			}(id, msg.Method, msg.Params)
		case msg.ID != nil:
			c.mu.Lock()
			ch := c.pending[msg.ID.key()]
			delete(c.pending, msg.ID.key())
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		case msg.Method != "":
			c.onNotify(msg.Method, msg.Params)
		}
	}
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
