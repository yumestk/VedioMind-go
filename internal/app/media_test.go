package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureServer(t *testing.T) *Server {
	t.Helper()
	s := New(testDB(t), Config{Origin: "http://localhost:5173"})
	s.Resolver.Inspect = func(context.Context, string) ([]byte, error) {
		return []byte(`{"id":"dQw4w9WgXcQ","title":"fixture video","duration":10,"subtitles":{"zh":[{"ext":"json3","url":"https://www.youtube.com/captions"}]}}`), nil
	}
	s.Resolver.Client.Transport = fixtureTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"events":[{"tStartMs":0,"dDurationMs":5000,"segs":[{"utf8":"Go 是一种编译型语言。"}]},{"tStartMs":5000,"dDurationMs":5000,"segs":[{"utf8":"并发可以使用 goroutine。"}]}]}`)), Header: make(http.Header)}, nil
	})
	return s
}
func registered(t *testing.T, s *Server, email string) *browser {
	t.Helper()
	b := &browser{handler: s.Routes()}
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	status(t, b.request("POST", "/api/auth/register", `{"email":"`+email+`","password":"long-test-password"}`), 201)
	return b
}
func TestImportAtomicityOwnershipAndDuplicates(t *testing.T) {
	s := fixtureServer(t)
	a := registered(t, s, "a@example.com")
	b := registered(t, s, "b@example.com")
	input := `{"url":"https://youtu.be/dQw4w9WgXcQ"}`
	w := a.request("POST", "/api/media/import-url", input)
	status(t, w, 201)
	var m Media
	if json.Unmarshal(w.Body.Bytes(), &m) != nil || m.ID == "" {
		t.Fatal(w.Body.String())
	}
	status(t, a.request("GET", "/api/media/"+m.ID, ""), 200)
	status(t, a.request("GET", "/api/media/"+m.ID+"/transcript", ""), 200)
	status(t, b.request("GET", "/api/media/"+m.ID, ""), 404)
	status(t, b.request("GET", "/api/media/"+m.ID+"/transcript", ""), 404)
	status(t, b.request("DELETE", "/api/media/"+m.ID, "{}"), 404)
	status(t, a.request("POST", "/api/media/import-url", input), 200)
	// Concurrent imports both see one complete snapshot, without duplicate segments.
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { clone := *b; codes <- clone.request("POST", "/api/media/import-url", input).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 && code != 201 {
			t.Fatal(code)
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM media`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("media %d %v", count, err)
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM transcript_segments`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("segments %d %v", count, err)
	}
	status(t, a.request("DELETE", "/api/media/"+m.ID, "{}"), 204)
	status(t, a.request("GET", "/api/media/"+m.ID, ""), 404)
	s.Resolver.Inspect = func(context.Context, string) ([]byte, error) {
		return []byte(`{"id":"aaaaaaaaaaa","title":"no subtitles"}`), nil
	}
	w = a.request("POST", "/api/media/import-url", `{"url":"https://youtu.be/aaaaaaaaaaa"}`)
	status(t, w, 422)
	if err := s.DB.QueryRow(`SELECT count(*) FROM media WHERE external_id='aaaaaaaaaaa'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial import", count, err)
	}
}
