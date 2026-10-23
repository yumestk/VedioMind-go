package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This test-only process uses a real isolated PostgreSQL schema and fixture HTTP
// responses. Production code never enables a fake-success fallback.
func TestBrowserServer(t *testing.T) {
	if os.Getenv("E2E_SERVER") != "1" {
		t.Skip("explicit browser fixture server only")
	}
	s := fixtureServer(t)
	s.Config.Origin = "http://127.0.0.1:15173"
	if os.Getenv("E2E_PRODUCTION") == "1" {
		s.Config.Origin = "http://127.0.0.1:18080"
		root, err := filepath.Abs("../../web/dist")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("WEB_DIST", root)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) == 0 {
			w.WriteHeader(400)
			return
		}
		system := body.Messages[0].Content
		content := "## 核心观点\n\nGo 是编译型语言，支持 goroutine 并发。\n\n<img src=x onerror=\"window.__xss=1\"><script>window.__xss=1</script>"
		if strings.Contains(system, "chapters") {
			content = `{"chapters":[{"title":"Go 基础","startSegmentId":1},{"title":"并发模型","startSegmentId":2}]}`
		} else if strings.Contains(system, "segmentIds") {
			content = `{"answer":"可以使用 goroutine 实现并发。","segmentIds":[2]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
	}))
	defer model.Close()
	s.AI.URL = model.URL
	s.AI.Key = "fixture-only"
	s.AI.Model = "fixture"
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: s.Routes()}
	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	defer s.WaitWorkers()
	defer server.Close()
	s.RunWorkers(ctx)
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	t.Log("browser fixture API ready on 18080")
	select {
	case <-ctx.Done():
	case err := <-errs:
		done()
		if err != http.ErrServerClosed {
			t.Error(err)
		}
	}
}
