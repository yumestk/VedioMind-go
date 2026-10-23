package app

import (
	"context"
	"encoding/json"
	"github.com/yumestk/VedioMind-go/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChapterValidation(t *testing.T) {
	segments := []platform.Segment{{ID: 1, StartMs: 0, EndMs: 1000}, {ID: 2, StartMs: 1000, EndMs: 2000}, {ID: 3, StartMs: 2000, EndMs: 3000}}
	valid := `{"chapters":[{"title":"开头","startSegmentId":1},{"title":"重点","startSegmentId":3}]}`
	chapters, err := parseChapters(valid, segments)
	if err != nil || len(chapters) != 2 || chapters[0].EndMs != 2000 || chapters[1].EndMs != 3000 {
		t.Fatal(chapters, err)
	}
	cases := []string{`{}`, `{"chapters":[]}`, `{"chapters":[{"title":"跳过开头","startSegmentId":2}]}`, `{"chapters":[{"title":"x","startSegmentId":1.1}]}`, `{"chapters":[{"title":"x","startSegmentId":99}]}`, `{"chapters":[{"title":" ","startSegmentId":1}]}`, `{"chapters":[{"title":"x","startSegmentId":1},{"title":"y","startSegmentId":1}]}`, `{"chapters":[{"title":"x","startSegmentId":1},{"title":"y","startSegmentId":3},{"title":"z","startSegmentId":2}]}`, `{"chapters":[{"title":"` + strings.Repeat("长", 101) + `","startSegmentId":1}]}`}
	for _, raw := range cases {
		if _, err = parseChapters(raw, segments); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err = parseChapters(`{"chapters":[{"title":"x","startSegmentId":1}]}`, []platform.Segment{{ID: 1, StartMs: 0, EndMs: 0}}); err == nil {
		t.Fatal("zero duration")
	}
}
func TestChapterPipelineReusesSummary(t *testing.T) {
	s := fixtureServer(t)
	b := registered(t, s, "chapters@example.com")
	m := importFixture(t, s, b)
	ctx := context.Background()
	// Represents a successfully completed P3 pipeline-v1 summary.
	if _, err := s.DB.Exec(`UPDATE analysis_jobs SET status='SUCCEEDED',pipeline_version=1; INSERT INTO analysis_artifacts(media_id,snapshot_version,kind,processor_version,payload) SELECT id,1,'SUMMARY',1,'"old summary"'::jsonb FROM media`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	invalid := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		if !strings.Contains(request.Messages[0].Content, "chapters") {
			t.Error("summary re-generated")
		}
		content := `{"chapters":[{"title":"Go 基础","startSegmentId":1},{"title":"并发","startSegmentId":2}]}`
		if invalid {
			content = `{"chapters":[{"title":"虚构","startSegmentId":99}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
	}))
	defer ts.Close()
	s.AI.URL = ts.URL
	s.AI.Key = "fixture"
	s.AI.Model = "fixture"
	status(t, b.request("POST", "/api/media/"+m.ID+"/analyses", "{}"), 202)
	j, err := s.claim(ctx)
	if err != nil || j == nil {
		t.Fatal(err)
	}
	s.execute(ctx, j)
	var state string
	s.DB.QueryRow(`SELECT status FROM analysis_jobs WHERE id=$1`, j.ID).Scan(&state)
	if state != "FAILED" {
		t.Fatal(state)
	}
	var summary string
	s.DB.QueryRow(`SELECT payload#>>'{}' FROM analysis_artifacts WHERE kind='SUMMARY'`).Scan(&summary)
	if summary != "old summary" {
		t.Fatal(summary)
	}
	invalid = false
	status(t, b.request("POST", "/api/media/"+m.ID+"/analyses", "{}"), 202)
	j, err = s.claim(ctx)
	if err != nil || j == nil {
		t.Fatal(err)
	}
	s.execute(ctx, j)
	items, err := s.chapters(ctx, m.ID, 1)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	status(t, b.request("POST", "/api/media/"+m.ID+"/analyses", "{}"), 200)
	var count int
	s.DB.QueryRow(`SELECT count(*) FROM analysis_jobs WHERE pipeline_version=1 AND status='SUCCEEDED'`).Scan(&count)
	if count != 1 {
		t.Fatal("old terminal job mutated")
	}
}
