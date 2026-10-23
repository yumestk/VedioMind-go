package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yumestk/VedioMind-go/internal/ai"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func importFixture(t *testing.T, s *Server, b *browser) Media {
	t.Helper()
	w := b.request("POST", "/api/media/import-url", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`)
	status(t, w, 201)
	var m Media
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func fakeAI(s *Server, statusCode int, content string) (*httptest.Server, *atomic.Int32) {
	calls := new(atomic.Int32)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(statusCode)
		if statusCode == 200 {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
		}
	}))
	s.AI = &ai.Client{URL: ts.URL, Key: "fixture-key", Model: "fixture", Budget: 48000, Output: 1000, HTTP: &http.Client{Timeout: time.Second}}
	return ts, calls
}
func TestAnalysisLeaseCheckpointAndOwnership(t *testing.T) {
	s := fixtureServer(t)
	b := registered(t, s, "worker@example.com")
	other := registered(t, s, "other@example.com")
	m := importFixture(t, s, b)
	ctx := context.Background()
	ts, calls := fakeAI(s, 200, "## 总结\nGo 与并发。")
	defer ts.Close()
	// Two claimers cannot own the same active job.
	var wg sync.WaitGroup
	claimed := make(chan *Job, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { j, err := s.claim(ctx); claimed <- j; errs <- err })
	}
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var j *Job
	for item := range claimed {
		if item != nil {
			if j != nil {
				t.Fatal("double claim")
			}
			j = item
		}
	}
	if j == nil {
		t.Fatal("no job")
	}
	status(t, other.request("GET", "/api/analysis-jobs/"+j.ID, ""), 404)
	firstDeadline := j.deadline
	if _, err := s.DB.Exec(`UPDATE analysis_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.saveArtifact(ctx, j, "SUMMARY", []byte(`"stale"`), true); !errors.Is(err, errLeaseLost) {
		t.Fatal("stale artifact accepted", err)
	}
	takeover, err := s.claim(ctx)
	if err != nil || takeover == nil {
		t.Fatal(takeover, err)
	}
	if !takeover.deadline.Equal(firstDeadline) {
		t.Fatal("deadline reset")
	}
	s.execute(ctx, takeover)
	var state string
	if err = s.DB.QueryRow(`SELECT status FROM analysis_jobs WHERE id=$1`, j.ID).Scan(&state); err != nil || state != "SUCCEEDED" {
		t.Fatal(state, err)
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected model calls", calls.Load())
	}
	status(t, b.request("GET", "/api/media/"+m.ID+"/analysis", ""), 200)
	status(t, b.request("POST", "/api/media/"+m.ID+"/analyses", "{}"), 200)
	var count int
	s.DB.QueryRow(`SELECT count(*) FROM analysis_artifacts`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestAnalysisBudgetAndRetryPersistence(t *testing.T) {
	for _, mode := range []string{"context", "transient", "permanent", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			s := fixtureServer(t)
			b := registered(t, s, mode+"@example.com")
			importFixture(t, s, b)
			code := 503
			if mode == "permanent" {
				code = 401
			}
			ts, calls := fakeAI(s, code, "")
			defer ts.Close()
			ctx := context.Background()
			if mode == "context" {
				s.AI.Budget = 1
			}
			j, err := s.claim(ctx)
			if err != nil || j == nil {
				t.Fatal(err)
			}
			if mode == "deadline" {
				_, err = s.DB.Exec(`UPDATE analysis_jobs SET status='RETRY_WAIT',deadline_at=clock_timestamp()-interval '1 second',next_run_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, j.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.claim(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				s.execute(ctx, j)
			}
			if mode == "transient" {
				for range 2 {
					if _, err = s.DB.Exec(`UPDATE analysis_jobs SET next_run_at=clock_timestamp() WHERE id=$1`, j.ID); err != nil {
						t.Fatal(err)
					}
					j, err = s.claim(ctx)
					if err != nil || j == nil {
						t.Fatal(err)
					}
					s.execute(ctx, j)
				}
			}
			var state string
			var total int
			if err = s.DB.QueryRow(`SELECT status,total_calls FROM analysis_jobs WHERE id=$1`, j.ID).Scan(&state, &total); err != nil {
				t.Fatal(err)
			}
			if state != "FAILED" {
				t.Fatal(mode, state)
			}
			want := int32(1)
			if mode == "transient" {
				want = 3
			}
			if mode == "context" || mode == "deadline" {
				want = 0
			}
			if calls.Load() != want || total != int(want) {
				t.Fatalf("%s calls=%d total=%d want=%d", mode, calls.Load(), total, want)
			}
		})
	}
}
