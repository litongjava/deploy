package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"deploy-cli/internal/api"
)

// Conn 是一帧一帧读取日志流的连接抽象（便于单元测试替换）。
type Conn interface {
	ReadFrame() (*Frame, error)
	Close() error
}

// DefaultHandshakeTimeout 是 WebSocket 握手超时。
const DefaultHandshakeTimeout = 10 * time.Second

// Dialer 通过 gorilla/websocket 连接契约 7.4 的日志端点。
type Dialer struct {
	Client           *api.Client
	Header           http.Header
	HandshakeTimeout time.Duration
}

// NewDialer 构造基于 api.Client 的连接器。
func NewDialer(c *api.Client) *Dialer {
	return &Dialer{Client: c, HandshakeTimeout: DefaultHandshakeTimeout}
}

// StreamLogs 建立到 /api/v1/ws/runs/{id}?token=...&offset=n 的连接。
func (d *Dialer) StreamLogs(ctx context.Context, runID, offset int64) (Conn, error) {
	if d.Client == nil {
		return nil, fmt.Errorf("ws: missing api.Client")
	}
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	endpoint := d.Client.WSURL("/ws/runs/"+strconv.FormatInt(runID, 10), q)

	ht := d.HandshakeTimeout
	if ht <= 0 {
		ht = DefaultHandshakeTimeout
	}
	dialer := &websocket.Dialer{
		HandshakeTimeout: ht,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, resp, err := dialer.DialContext(ctx, endpoint, d.Header)
	if err != nil {
		if resp != nil {
			return nil, &api.Error{
				HTTPStatus: resp.StatusCode,
				Code:       codeFromStatus(resp.StatusCode),
				Message:    fmt.Sprintf("WebSocket handshake failed (HTTP %d): %v", resp.StatusCode, err),
			}
		}
		return nil, api.TransportError("open WebSocket connection", err)
	}
	return &gorillaConn{conn: conn}, nil
}

func codeFromStatus(status int) int {
	switch status {
	case http.StatusUnauthorized:
		return api.CodeUnauthorized
	case http.StatusForbidden:
		return api.CodeForbidden
	case http.StatusNotFound:
		return api.CodeNotFound
	default:
		return 0
	}
}

type gorillaConn struct {
	conn *websocket.Conn
}

func (g *gorillaConn) ReadFrame() (*Frame, error) {
	_, data, err := g.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("cannot parse WebSocket frame: %w", err)
	}
	return &f, nil
}

func (g *gorillaConn) Close() error { return g.conn.Close() }
