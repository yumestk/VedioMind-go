package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/yumestk/VedioMind-go/migrations"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for real PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close(); admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	if err = migrations.Up(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

type browser struct {
	handler http.Handler
	csrf    string
	session string
}

func (b *browser) request(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:5173")
	if b.csrf != "" {
		r.AddCookie(&http.Cookie{Name: "vm_csrf", Value: b.csrf})
		r.Header.Set("X-CSRF-Token", b.csrf)
	}
	if b.session != "" {
		r.AddCookie(&http.Cookie{Name: "vm_session", Value: b.session})
	}
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == "vm_csrf" {
			b.csrf = c.Value
		}
		if c.Name == "vm_session" {
			b.session = c.Value
		}
	}
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
	}
}
func TestAuthentication(t *testing.T) {
	db := testDB(t)
	s := New(db, Config{Origin: "http://localhost:5173"})
	b := browser{handler: s.Routes()}
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	credentials := `{"email":" Example@Example.com ","password":"a-long-test-password"}`
	status(t, b.request("POST", "/api/auth/register", credentials), 201)
	original := b.session
	if original == "" {
		t.Fatal("missing session")
	}
	w := b.request("GET", "/api/auth/me", "")
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "password") {
		t.Fatal("secret leaked")
	}
	var body struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.User.Email != "example@example.com" {
		t.Fatalf("user %+v %v", body, err)
	}
	// A different server instance sees the persisted session.
	b.handler = New(db, s.Config).Routes()
	status(t, b.request("GET", "/api/auth/me", ""), 200)
	status(t, b.request("POST", "/api/auth/login", credentials), 200)
	if b.session == original {
		t.Fatal("session not rotated")
	}
	current := b.session
	b.session = original
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	b.session = current
	status(t, b.request("POST", "/api/auth/logout", "{}"), 204)
	b.session = current
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	b.session = strings.Repeat("f", 64)
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	status(t, b.request("POST", "/api/auth/login", credentials), 200)
	if _, err := db.Exec(`UPDATE sessions SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	status(t, b.request("GET", "/api/auth/me", ""), 401)
	status(t, b.request("POST", "/api/auth/login", `{"email":"example@example.com","password":"wrong-long-password"}`), 401)
	noCSRF := browser{handler: b.handler}
	status(t, noCSRF.request("POST", "/api/auth/login", credentials), 403)
	req := httptest.NewRequest("POST", "/api/auth/logout", bytes.NewBufferString("{}"))
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: "vm_csrf", Value: b.csrf})
	req.Header.Set("X-CSRF-Token", b.csrf)
	out := httptest.NewRecorder()
	b.handler.ServeHTTP(out, req)
	status(t, out, 403)
	// Seed an exhausted source window so bcrypt runtime and a minute rollover
	// cannot make this assertion probabilistic. Include the next window too.
	if _, err := db.Exec(`INSERT INTO rate_limit_counters(scope,window_start,count)
 SELECT 'auth:ip:192.0.2.1',floor(extract(epoch from clock_timestamp())/60)::bigint+window_offset,10
 FROM generate_series(0,1) AS window_offset
 ON CONFLICT(scope,window_start) DO UPDATE SET count=10`); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"email":"absent@example.com","password":"wrong-long-password"}`))
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.77")
	req.Header.Set("X-CSRF-Token", b.csrf)
	req.AddCookie(&http.Cookie{Name: "vm_csrf", Value: b.csrf})
	w = httptest.NewRecorder()
	b.handler.ServeHTTP(w, req)
	status(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry-after")
	}
}
func TestConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("PUBLIC_ORIGIN", "http://example.com")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted insecure production")
	}
	t.Setenv("PUBLIC_ORIGIN", "https://example.com")
	c, err := LoadConfig()
	if err != nil || !c.SecureCookie {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:5173")
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1, 10.10.0.0/16")
	cfg, err := LoadConfig()
	if err != nil || len(cfg.TrustedProxies) != 2 {
		t.Fatal(cfg, err)
	}
	t.Setenv("TRUSTED_PROXIES", "not-a-network")
	if _, err = LoadConfig(); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}
