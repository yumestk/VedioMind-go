package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionStaticRouting(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>React application</html>", "assets/app.js": "console.log('app')"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WEB_DIST", dir)
	s := New(nil, Config{Origin: "https://app.example.com", SecureCookie: true})
	router := s.Routes()
	for _, test := range []struct {
		path     string
		status   int
		contains string
	}{{"/", 200, "React application"}, {"/media/123", 200, "React application"}, {"/assets/app.js", 200, "console.log"}, {"/api/unknown", 404, "NOT_FOUND"}, {"/api", 404, "NOT_FOUND"}, {"/assets/missing.js", 404, "NOT_FOUND"}, {"/.env", 404, "NOT_FOUND"}} {
		t.Run(test.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.contains) {
				t.Fatal(w.Code, w.Body.String())
			}
			if test.status == 200 && w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
		})
	}
}
