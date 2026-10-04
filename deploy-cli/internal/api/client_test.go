package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deploy-cli/internal/exitcode"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:8080":              "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/":             "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/api/v1":       "http://127.0.0.1:8080",
		"https://deploy.example.com/api/v1/": "https://deploy.example.com",
		"127.0.0.1:8080":                     "http://127.0.0.1:8080",
		"":                                   "",
	}
	for in, want := range cases {
		if got := NormalizeBaseURL(in); got != want {
			t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURLAndWSURL(t *testing.T) {
	c := New("http://127.0.0.1:8080/api/v1", "tok")
	if got := c.URL("/auth/me", nil); got != "http://127.0.0.1:8080/api/v1/auth/me" {
		t.Fatalf("URL = %q", got)
	}
	ws := c.WSURL("/ws/runs/88", map[string][]string{"offset": {"0"}})
	if !strings.HasPrefix(ws, "ws://127.0.0.1:8080/api/v1/ws/runs/88?") {
		t.Fatalf("WSURL = %q", ws)
	}
	if !strings.Contains(ws, "token=tok") || !strings.Contains(ws, "offset=0") {
		t.Fatalf("WSURL 缺少参数: %q", ws)
	}
	cs := New("https://deploy.example.com", "")
	if got := cs.WSURL("/ws/runs/1", nil); !strings.HasPrefix(got, "wss://deploy.example.com/api/v1/ws/runs/1") {
		t.Fatalf("WSURL(https) = %q", got)
	}
}

func TestLoginSuccessAndRawTokenHeader(t *testing.T) {
	var gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			gotCT = r.Header.Get("Content-Type")
			var req LoginRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Username != "admin" || req.Password != "secret" {
				t.Errorf("请求体 = %+v", req)
			}
			writeEnvelope(w, 200, map[string]any{
				"token": "jwt-abc", "expiresAt": 1760000000,
				"mustChangePassword": false,
				"user":               User{ID: 1, Username: "admin", Role: "admin"},
			})
		case "/api/v1/auth/me":
			if gotAuth == "" {
				t.Errorf("GET /auth/me 缺少 Authorization")
			}
			writeEnvelope(w, 200, User{ID: 1, Username: "admin", Role: "admin"})
		default:
			t.Errorf("path = %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	res, err := c.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.Token != "jwt-abc" || res.User.Username != "admin" {
		t.Fatalf("结果 = %+v", res)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	if gotAuth != "" {
		t.Fatalf("登录前不应带 Authorization，得到 %q", gotAuth)
	}

	// 带 token 的请求必须是裸 token（契约 0.3）
	c2 := New(srv.URL, "jwt-abc")
	if _, err := c2.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if gotAuth != "jwt-abc" {
		t.Fatalf("Authorization = %q, want 裸 token", gotAuth)
	}
}

func TestBusinessErrorOnHTTP200(t *testing.T) {
	// 契约 0.1：业务失败也是 HTTP 200，靠 success=false + errorCode 区分。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":false,"errorCode":40001,"errorMessage":"项目 key 已存在","showType":2}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	_, err := c.CreateProject(context.Background(), &ProjectInput{Key: "dup"})
	if err == nil {
		t.Fatal("期望业务错误")
	}
	e, ok := AsError(err)
	if !ok {
		t.Fatalf("错误类型 = %T", err)
	}
	if e.Code != 40001 || e.Message != "项目 key 已存在" || e.ShowType != 2 {
		t.Fatalf("错误 = %+v", e)
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Usage)
	}
	if e.IsNotFound() || e.IsAuth() || e.IsRetriable() {
		t.Fatalf("分类错误: %+v", e)
	}
}

func TestHTTP401MapsToAuthExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errorCode":40100,"errorMessage":"未登录或登录已过期","showType":0}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "expired")
	_, err := c.Me(context.Background())
	if err == nil {
		t.Fatal("期望 401 错误")
	}
	e, _ := AsError(err)
	if !e.IsAuth() {
		t.Fatalf("应判定为鉴权失败: %+v", e)
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Auth)
	}
	if !strings.Contains(err.Error(), "未登录或登录已过期") {
		t.Fatalf("错误信息 = %q", err.Error())
	}
}

func TestHTTP500MapsToNetworkExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errorCode":50000,"errorMessage":"内部错误","showType":2}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	_, err := c.Doctor(context.Background())
	if err == nil {
		t.Fatal("期望 5xx 错误")
	}
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Network)
	}
	e, _ := AsError(err)
	if !e.IsRetriable() {
		t.Fatalf("5xx 应可重试: %+v", e)
	}
}

func TestNotFoundWithoutEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	_, err := c.GetProject(context.Background(), "ghost")
	e, ok := AsError(err)
	if !ok {
		t.Fatalf("错误类型 = %T (%v)", err, err)
	}
	if !e.IsNotFound() {
		t.Fatalf("应判定为不存在: %+v", e)
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Usage)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("错误信息不应含换行: %q", err.Error())
	}
}

func TestTransportErrorWhenServerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立刻关闭 → 连接被拒

	c := New(url, "t")
	_, err := c.Healthz(context.Background())
	if err == nil {
		t.Fatal("期望传输错误")
	}
	e, ok := AsError(err)
	if !ok || !e.IsTransport() {
		t.Fatalf("应判定为传输错误: %#v", err)
	}
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Fatalf("退出码 = %d, want %d", got, exitcode.Network)
	}
}

func TestRunLogQueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/api/v1/runs/88/log" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeEnvelope(w, 200, LogChunk{Offset: 0, NextOffset: 4, EOF: true, Data: "abcd"})
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	chunk, err := c.RunLog(context.Background(), 88, 0, 65536)
	if err != nil {
		t.Fatalf("RunLog: %v", err)
	}
	if chunk.Data != "abcd" || !chunk.EOF || chunk.NextOffset != 4 {
		t.Fatalf("chunk = %+v", chunk)
	}
	if !strings.Contains(gotQuery, "offset=0") || !strings.Contains(gotQuery, "limit=65536") {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestPagedProjectsAndListQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeEnvelope(w, 200, map[string]any{
			"list": []Project{
				{ID: 1, Key: "my-app", Name: "我的应用", Workdir: "/srv/w/my-app", LastRunStatus: "success"},
			},
			"total": 1, "page": 2, "pageSize": 20,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	archived := false
	page, err := c.ListProjects(context.Background(), 2, 20, "my", &archived)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if page.Total != 1 || len(page.List) != 1 || page.List[0].Key != "my-app" || page.Page != 2 {
		t.Fatalf("page = %+v", page)
	}
	for _, want := range []string{"page=2", "pageSize=20", "keyword=my", "archived=false"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q 缺少 %q", gotQuery, want)
		}
	}
}

func TestNullDataIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":null}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("data=null 应视为成功: %v", err)
	}
	if err := c.CancelRun(context.Background(), 1); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
}

func TestProtocolErrorOnBrokenJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	_, err := c.Me(context.Background())
	if err == nil {
		t.Fatal("期望协议错误")
	}
	if got := exitcode.Of(err); got != exitcode.Network {
		t.Fatalf("退出码 = %d, want %d（服务端响应异常归为服务不可用）", got, exitcode.Network)
	}
}

func TestIsTerminalStatus(t *testing.T) {
	for _, s := range []string{"success", "failed", "canceled", "timeout", "error"} {
		if !IsTerminalStatus(s) {
			t.Fatalf("%q 应为终态", s)
		}
	}
	for _, s := range []string{"queued", "running", ""} {
		if IsTerminalStatus(s) {
			t.Fatalf("%q 不应为终态", s)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{0: "-", 500: "500ms", 8123: "8.1s", 125000: "2m5s"}
	for in, want := range cases {
		if got := FormatDuration(in); got != want {
			t.Fatalf("FormatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func writeEnvelope(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"success": true}
	if data != nil {
		body["data"] = data
	}
	_ = json.NewEncoder(w).Encode(body)
}
