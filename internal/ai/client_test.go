package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientBudgetAndProviderContract(t *testing.T) {
	calls := 0
	body := `{"choices":[{"finish_reason":"stop","message":{"content":"a summary"}}]}`
	status := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fake-test-key" {
			t.Error("missing authorization")
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer ts.Close()
	c := &Client{URL: ts.URL, Key: "fake-test-key", Model: "fixture", Budget: 4096, Output: 100, HTTP: &http.Client{Timeout: time.Second}}
	messages := []Message{{Role: "user", Content: strings.Repeat("a", 5000)}}
	if _, err := c.Complete(context.Background(), messages, false); err == nil || calls != 0 {
		t.Fatal("overbudget called provider", calls, err)
	}
	messages[0].Content = "valid prompt"
	if answer, err := c.Complete(context.Background(), messages, false); err != nil || answer != "a summary" {
		t.Fatal(answer, err)
	}
	body = `{"choices":[{"finish_reason":"length","message":{"content":"{\"valid\":true}"}}]}`
	if _, err := c.Complete(context.Background(), messages, true); err == nil {
		t.Fatal("accepted truncated output")
	}
	status = 401
	_, err := c.Complete(context.Background(), messages, false)
	var e *Error
	if !errors.As(err, &e) || e.Retry {
		t.Fatal("permanent rejection retried", err)
	}
	status = 429
	_, err = c.Complete(context.Background(), messages, false)
	if !errors.As(err, &e) || !e.Retry {
		t.Fatal("transient error lost", err)
	}
}
