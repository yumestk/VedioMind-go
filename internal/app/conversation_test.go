package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yumestk/VedioMind-go/internal/ai"
	"github.com/yumestk/VedioMind-go/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAnswerValidation(t *testing.T) {
	segments := []platform.Segment{}
	for i := 1; i <= 7; i++ {
		segments = append(segments, platform.Segment{ID: i, Text: "原文", StartMs: int64(i * 1000), EndMs: int64(i*1000 + 500)})
	}
	answer, refs, err := parseAnswer(`{"answer":"依据原文回答","segmentIds":[2,1,2,3,4,5,6,7]}`, segments)
	if err != nil || answer != "依据原文回答" || len(refs) != 5 || refs[0].SegmentID != 2 || refs[0].Text != "原文" || refs[0].StartMs != 2000 {
		t.Fatal(answer, refs, err)
	}
	answer, refs, err = parseAnswer(`{"answer":"试图补充外部知识","segmentIds":[]}`, segments)
	if err != nil || answer != noEvidence || len(refs) != 0 {
		t.Fatal(answer, refs, err)
	}
	for _, raw := range []string{`{"answer":"x","segmentIds":[1.5]}`, `{"answer":"x","segmentIds":[999]}`, `{"answer":"x","segmentIds":[null]}`, `{"answer":" ","segmentIds":[1]}`, `not json`, `{"answer":"x","segmentIds":null}`, `{"answer":"x"}`} {
		if _, _, err := parseAnswer(raw, segments); err == nil {
			t.Fatal(raw)
		}
	}
}
func questionBody(question, conversation, key string) string {
	b, _ := json.Marshal(Question{Question: question, ConversationID: conversation, RequestID: key})
	return string(b)
}

type chatResult struct {
	ConversationID string        `json:"conversationId"`
	Messages       []ChatMessage `json:"messages"`
}

func TestQuestionHistoryReplayIsolationAndBudget(t *testing.T) {
	s := fixtureServer(t)
	b := registered(t, s, "qa@example.com")
	other := registered(t, s, "other@example.com")
	m := importFixture(t, s, b)
	var calls atomic.Int32
	var secondSawHistory atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var body struct {
			Messages []ai.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if n == 2 && len(body.Messages) == 4 && body.Messages[1].Role == "user" && body.Messages[2].Role == "assistant" && strings.Contains(body.Messages[0].Content, "goroutine") {
			secondSawHistory.Store(true)
		}
		content := `{"answer":"Go 使用 goroutine 实现并发。","segmentIds":[2]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
	}))
	defer ts.Close()
	s.AI.URL = ts.URL
	s.AI.Key = "fixture-key"
	s.AI.Model = "fixture"
	s.AI.Output = 1000
	first := questionBody("如何实现并发？", "", "11111111-1111-4111-8111-111111111111")
	w := b.request("POST", "/api/media/"+m.ID+"/questions", first)
	status(t, w, 200)
	var result chatResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Messages) != 2 {
		t.Fatal(w.Body.String(), err)
	}
	id := result.ConversationID
	if len(result.Messages[1].Citations) != 1 || result.Messages[1].Citations[0].Text != "并发可以使用 goroutine。" {
		t.Fatal(result)
	}
	// Lost first response: same original empty conversation parameter returns same round.
	replay := b.request("POST", "/api/media/"+m.ID+"/questions", first)
	status(t, replay, 200)
	if replay.Body.String() != w.Body.String() || calls.Load() != 1 {
		t.Fatal("not replayed", replay.Body.String())
	}
	status(t, b.request("POST", "/api/media/"+m.ID+"/questions", questionBody("不同问题", "", "11111111-1111-4111-8111-111111111111")), 409)
	status(t, b.request("POST", "/api/media/"+m.ID+"/questions", questionBody("这意味着什么？", id, "22222222-2222-4222-8222-222222222222")), 200)
	if !secondSawHistory.Load() {
		t.Fatal("full transcript or successful history missing")
	}
	status(t, other.request("GET", "/api/conversations/"+id+"/messages", ""), 404)
	status(t, other.request("POST", "/api/media/"+m.ID+"/questions", first), 404)
	status(t, b.request("GET", "/api/conversations/"+id+"/messages", ""), 200)
	s.AI.Budget = 1
	before := calls.Load()
	status(t, b.request("POST", "/api/media/"+m.ID+"/questions", questionBody("预算检查", id, "33333333-3333-4333-8333-333333333333")), 422)
	if calls.Load() != before {
		t.Fatal("overbudget called model")
	}
	var count int
	s.DB.QueryRow(`SELECT count(*) FROM messages`).Scan(&count)
	if count != 4 {
		t.Fatal("half round persisted", count)
	}
}
func TestConversationLeaseAndExpiredRelease(t *testing.T) {
	s := fixtureServer(t)
	b := registered(t, s, "concurrency@example.com")
	m := importFixture(t, s, b)
	u, _ := s.currentForTest(b)
	ctx := context.Background()
	first := Question{Question: "first", RequestID: "11111111-1111-4111-8111-111111111111"}
	old, err := s.beginQuestion(ctx, m, u, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.beginQuestion(ctx, m, u, first); !errors.Is(err, errBusy) {
		t.Fatal("inflight duplicate", err)
	}
	second := Question{Question: "second", ConversationID: old.ConversationID, RequestID: "22222222-2222-4222-8222-222222222222"}
	if _, err = s.beginQuestion(ctx, m, u, second); !errors.Is(err, errBusy) {
		t.Fatal("same conversation not serialized", err)
	}
	// Another request takes over after expiry; stale commit and release are fenced.
	if _, err = s.DB.Exec(`UPDATE conversations SET occupancy_until=clock_timestamp()-interval '1 second'; UPDATE question_requests SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	newer, err := s.beginQuestion(ctx, m, u, second)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.commitAnswer(ctx, old, first, "stale", nil); !errors.Is(err, errBusy) {
		t.Fatal("old commit accepted", err)
	}
	s.releaseQuestion(old)
	var token string
	if err = s.DB.QueryRow(`SELECT occupancy_token FROM conversations WHERE id=$1`, newer.ConversationID).Scan(&token); err != nil || token != newer.Token {
		t.Fatal("old release cleared new owner", token, err)
	}
	if err = s.commitAnswer(ctx, newer, second, noEvidence, nil); err != nil {
		t.Fatal(err)
	}
	s.releaseQuestion(newer)
	// A failed first question remains bound to its originally created conversation.
	retry, err := s.beginQuestion(ctx, m, u, first)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ConversationID != old.ConversationID {
		t.Fatal("duplicate first conversation")
	}
	s.releaseQuestion(retry)
	// Cross-video and snapshot mismatch are rejected at the same access boundary.
	var otherMedia string
	if err = s.DB.QueryRow(`INSERT INTO media(owner_user_id,platform,external_id,canonical_url,title,duration_ms,source,language) VALUES($1,'YOUTUBE','aaaaaaaaaaa','https://youtube.com/watch?v=aaaaaaaaaaa','other',100,'manual','en') RETURNING id::text`, u.ID).Scan(&otherMedia); err != nil {
		t.Fatal(err)
	}
	mismatched := Question{Question: "x", ConversationID: old.ConversationID, RequestID: "33333333-3333-4333-8333-333333333333"}
	if _, err = s.beginQuestion(ctx, Media{ID: otherMedia, SnapshotVersion: 1}, u, mismatched); !errors.Is(err, errNotFound) {
		t.Fatal("cross video accepted", err)
	}
	if _, err = s.DB.Exec(`UPDATE conversations SET snapshot_version=2 WHERE id=$1`, old.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.beginQuestion(ctx, m, u, mismatched); !errors.Is(err, errNotFound) {
		t.Fatal("snapshot mismatch accepted", err)
	}
}
func (s *Server) currentForTest(b *browser) (User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT u.id::text,u.email FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=$1`, digest(b.session)).Scan(&u.ID, &u.Email)
	return u, err
}
