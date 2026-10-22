package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDependencyRequests(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		method, path string
		check        func(*testing.T, map[string]any)
	}{
		{"update preserves omitted fields", []string{"project", "update", "app", "--dependencies", "lib"}, "PUT", "/api/v1/projects/app", func(t *testing.T, b map[string]any) {
			if len(b) != 1 || b["dependencies"].([]any)[0] != "lib" {
				t.Fatalf("unexpected update: %v", b)
			}
		}},
		{"clear dependencies", []string{"project", "update", "app", "--dependencies="}, "PUT", "/api/v1/projects/app", func(t *testing.T, b map[string]any) {
			if d, ok := b["dependencies"].([]any); !ok || len(d) != 0 {
				t.Fatalf("not an empty array: %v", b)
			}
		}},
		{"run with dependencies", []string{"run", "app", "--with-dependencies"}, "POST", "/api/v1/projects/app/run", func(t *testing.T, b map[string]any) {
			if b["withDependencies"] != true {
				t.Fatal(b)
			}
		}},
		{"create library", []string{"project", "create", "--key", "lib", "--kind", "lib", "--dependencies", "base", "--workdir", "/srv/lib", "--command", "mvn install"}, "POST", "/api/v1/projects", func(t *testing.T, b map[string]any) {
			if b["kind"] != "lib" || b["dependencies"].([]any)[0] != "base" {
				t.Fatal(b)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				tc.check(t, body)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"success":true,"data":{"key":"app","runId":1,"status":"queued"}}`))
			}))
			defer server.Close()
			args := append([]string{"--address", server.URL, "--token", "test-token", "--json"}, tc.args...)
			code, _, err := exec(t, args...)
			if code != 0 || !called {
				t.Fatalf("code=%d called=%v %s", code, called, err)
			}
		})
	}
}
