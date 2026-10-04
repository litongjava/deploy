package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/version"
)

// APIPrefix 是契约规定的 Base path。
const APIPrefix = "/api/v1"

const (
	defaultTimeout = 60 * time.Second
	maxBodyBytes   = 32 << 20 // 32MiB，防止异常响应把内存吃满
)

// Client 是 API 契约的 HTTP 客户端。
type Client struct {
	// BaseURL 是不含 /api/v1 的站点根，例如 http://127.0.0.1:8080
	BaseURL   string
	Token     string
	HTTP      *http.Client
	UserAgent string
	Verbose   bool
	LogOut    io.Writer
}

// New 构造客户端；baseURL 允许带或不带 /api/v1 后缀，也允许省略 scheme。
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:   NormalizeBaseURL(baseURL),
		Token:     token,
		HTTP:      &http.Client{Timeout: defaultTimeout},
		UserAgent: "deploy-cli/" + version.Version,
	}
}

// NormalizeBaseURL 规范化服务端地址。
func NormalizeBaseURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	s = strings.TrimRight(s, "/")
	if strings.HasSuffix(s, APIPrefix) {
		s = strings.TrimSuffix(s, APIPrefix)
	}
	return strings.TrimRight(s, "/")
}

// URL 拼出完整的请求地址。
func (c *Client) URL(path string, query url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.BaseURL + APIPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// WSURL 拼出 WebSocket 地址（http→ws / https→wss），并带上 token 查询参数
// （浏览器 WebSocket 不能自定义 Header，契约 7.4 规定 token 走 query）。
func (c *Client) WSURL(path string, query url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base := c.BaseURL
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	if query == nil {
		query = url.Values{}
	}
	if c.Token != "" {
		query.Set("token", c.Token)
	}
	return base + APIPrefix + path + "?" + query.Encode()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Client) verbose(format string, args ...any) {
	if !c.Verbose || c.LogOut == nil {
		return
	}
	fmt.Fprintf(c.LogOut, "[debug] "+format+"\n", args...)
}

// envelope 是契约 0.1 的响应封装。
type envelope struct {
	Success      bool            `json:"success"`
	Data         json.RawMessage `json:"data"`
	ErrorCode    int             `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	ShowType     int             `json:"showType"`
}

// do 执行一次请求并把 data 解码到 out。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.BaseURL == "" {
		return exitcode.Usagef("未配置服务端地址")
	}
	endpoint := c.URL(path, query)

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return exitcode.Wrap(exitcode.Usage, err, "序列化请求体失败")
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "构造 HTTP 请求失败")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// 契约 0.3：CLI 使用裸 token（不带 Bearer 前缀）。
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	start := time.Now()
	resp, err := c.httpClient().Do(req)
	if err != nil {
		c.verbose("%s %s → 传输失败: %v", method, endpoint, err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return exitcode.Wrap(exitcode.Network, ctxErr, "%s %s 失败", method, endpoint)
		}
		return TransportError(fmt.Sprintf("请求 %s %s", method, endpoint), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return TransportError(fmt.Sprintf("读取 %s %s 的响应", method, endpoint), err)
	}
	c.verbose("%s %s → HTTP %d（%d 字节，耗时 %s）", method, endpoint, resp.StatusCode, len(raw), time.Since(start).Round(time.Millisecond))

	if len(raw) == 0 {
		if resp.StatusCode >= 400 {
			return NewError(resp.StatusCode, 0, fmt.Sprintf("HTTP %d（响应体为空）", resp.StatusCode))
		}
		return nil
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode >= 400 {
			return NewError(resp.StatusCode, 0, fmt.Sprintf("HTTP %d：%s", resp.StatusCode, snippet(raw)))
		}
		return ProtocolError(fmt.Sprintf("%s %s", method, endpoint), resp.StatusCode, err)
	}

	if !env.Success {
		code := env.ErrorCode
		if code == 0 {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				code = CodeUnauthorized
			case http.StatusForbidden:
				code = CodeForbidden
			case http.StatusNotFound:
				code = CodeNotFound
			}
		}
		msg := env.ErrorMessage
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return &Error{HTTPStatus: resp.StatusCode, Code: code, Message: msg, ShowType: env.ShowType}
	}

	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return ProtocolError(fmt.Sprintf("%s %s 的 data 字段", method, endpoint), resp.StatusCode, err)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		return "(空)"
	}
	return s
}

// ---------- 认证 ----------

// Login 调 POST /auth/login。
func (c *Client) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	var out LoginResult
	if err := c.do(ctx, http.MethodPost, "/auth/login", nil, LoginRequest{Username: username, Password: password}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Me 调 GET /auth/me。
func (c *Client) Me(ctx context.Context) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodGet, "/auth/me", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Logout 调 POST /auth/logout（服务端为无状态 JWT，失败可忽略）。
func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/auth/logout", nil, nil, nil)
}

// Healthz 调 GET /healthz（公开）。
func (c *Client) Healthz(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Doctor 调 GET /system/doctor。
func (c *Client) Doctor(ctx context.Context) (*Doctor, error) {
	var out Doctor
	if err := c.do(ctx, http.MethodGet, "/system/doctor", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- 项目 ----------

// ListProjects 调 GET /projects。
func (c *Client) ListProjects(ctx context.Context, page, pageSize int, keyword string, archived *bool) (*Page[Project], error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if archived != nil {
		q.Set("archived", strconv.FormatBool(*archived))
	}
	var out Page[Project]
	if err := c.do(ctx, http.MethodGet, "/projects", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProject 调 GET /projects/{key}。
func (c *Client) GetProject(ctx context.Context, key string) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(key), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateProject 调 POST /projects（commands/envs 内联）。
func (c *Client) CreateProject(ctx context.Context, in *ProjectInput) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, "/projects", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- 运行 ----------

// TriggerRun 调 POST /projects/{key}/run。
func (c *Client) TriggerRun(ctx context.Context, key string, req *RunRequest) (*RunTriggerResult, error) {
	if req == nil {
		req = &RunRequest{}
	}
	var out RunTriggerResult
	if err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(key)+"/run", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRun 调 GET /runs/{id}。
func (c *Client) GetRun(ctx context.Context, id int64) (*Run, error) {
	var out Run
	if err := c.do(ctx, http.MethodGet, "/runs/"+strconv.FormatInt(id, 10), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRuns 调 GET /runs（分页，可选 projectKey / status 过滤）。
func (c *Client) ListRuns(ctx context.Context, page, pageSize int, projectKey, status string) (*Page[Run], error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	if projectKey != "" {
		q.Set("projectKey", projectKey)
	}
	if status != "" {
		q.Set("status", status)
	}
	var out Page[Run]
	if err := c.do(ctx, http.MethodGet, "/runs", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunLog 调 GET /runs/{id}/log（增量拉取）。
func (c *Client) RunLog(ctx context.Context, id, offset int64, limit int) (*LogChunk, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out LogChunk
	if err := c.do(ctx, http.MethodGet, "/runs/"+strconv.FormatInt(id, 10)+"/log", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelRun 调 POST /runs/{id}/cancel（幂等）。
func (c *Client) CancelRun(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, "/runs/"+strconv.FormatInt(id, 10)+"/cancel", nil, nil, nil)
}
