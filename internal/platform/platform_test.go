package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestNormalizeBoundaries(t *testing.T) {
	r := New("", "")
	cases := []struct {
		raw, id string
		valid   bool
	}{
		{"https://www.bilibili.com/video/BV1xx411c7mD?share=1", "BV1xx411c7mD", true},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ", true}, {"https://youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ", true}, {"https://youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ", true},
		{"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ", "", false}, {"https://me@youtube.com/watch?v=dQw4w9WgXcQ", "", false}, {"https://youtube.com:4433/watch?v=dQw4w9WgXcQ", "", false}, {"file:///etc/passwd", "", false}, {"https://youtube.com/watch?v=bad", "", false}, {"https://youtu.be/dQw4w9WgXcQ/extra", "", false}, {strings.Repeat("x", 2001), "", false},
	}
	for _, c := range cases {
		t.Run(c.raw[:min(len(c.raw), 90)], func(t *testing.T) {
			target, err := r.Normalize(context.Background(), c.raw)
			if (err == nil) != c.valid || target.ID != c.id {
				t.Fatalf("%+v %v", target, err)
			}
		})
	}
}
func TestShortLinkCannotEscape(t *testing.T) {
	r := New("", "")
	calls := 0
	r.Client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		res := response(302, "")
		res.Request = req
		res.Header.Set("Location", "http://127.0.0.1/admin")
		return res, nil
	})
	_, err := r.Normalize(context.Background(), "https://b23.tv/abc")
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestBilibiliContract(t *testing.T) {
	r := New("", "")
	r.Client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/x/web-interface/view":
			return response(200, `{"code":0,"data":{"aid":1,"cid":2,"title":"演示视频","duration":12}}`), nil
		case "/x/v2/dm/view":
			return response(200, `{"code":0,"data":{"subtitle":{"subtitles":[{"lan":"ai-zh","type":1,"subtitle_url":"https://evil.example/a"},{"lan":"zh-CN","subtitle_url":"//aisubtitle.hdslb.com/caption"}]}}}`), nil
		case "/caption":
			if req.URL.Host != "aisubtitle.hdslb.com" {
				t.Fatal(req.URL)
			}
			return response(200, `{"body":[{"from":1.2345,"to":2.5,"content":" 原文 "},{"from":0,"to":0,"content":" "}]}`), nil
		}
		t.Fatalf("unexpected %s", req.URL)
		return nil, nil
	})
	target, _ := r.Normalize(context.Background(), "https://bilibili.com/video/BV1xx411c7mD")
	v, err := r.Resolve(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "BILIBILI_MANUAL" || len(v.Segments) != 1 || v.Segments[0].StartMs != 1235 || v.Segments[0].Text != "原文" {
		t.Fatalf("%+v", v)
	}
}
func TestYouTubeContract(t *testing.T) {
	r := New("", "")
	r.Inspect = func(ctx context.Context, canonical string) ([]byte, error) {
		if canonical != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatal(canonical)
		}
		return []byte(`{"id":"dQw4w9WgXcQ","title":"YouTube 示例","duration":6,"language":"en","subtitles":{"en":[{"ext":"json3","url":"https://www.youtube.com/en"}],"zh-Hans":[{"ext":"json3","url":"https://www.youtube.com/zh"}]},"automatic_captions":{"en-orig":[{"ext":"json3","url":"https://www.youtube.com/auto"}]}}`), nil
	}
	r.Client.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/zh" {
			t.Fatal("manual Chinese should win", req.URL)
		}
		return response(200, `{"events":[{"tStartMs":1000,"segs":[{"utf8":"hello\n"},{"utf8":" world"}]},{"tStartMs":3000,"dDurationMs":0,"segs":[{"utf8":"end"}]}]}`), nil
	})
	target, _ := r.Normalize(context.Background(), "https://youtube.com/watch?v=dQw4w9WgXcQ")
	v, err := r.Resolve(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if v.Language != "zh-Hans" || v.Source != "YOUTUBE_MANUAL" || v.Segments[0].Text != "hello world" || v.Segments[0].EndMs != 3000 || v.Segments[1].EndMs != 3001 {
		t.Fatalf("%+v", v)
	}
}
func TestUpstreamFailuresBounded(t *testing.T) {
	r := New("", "")
	for _, code := range []int{302, 429, 503} {
		r.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(code, "{}"), nil })
		var out any
		err := r.get(context.Background(), "https://www.youtube.com/caption", "", &out)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatal(err)
		}
		if code == 429 && e.Status != 429 {
			t.Fatal(e)
		}
	}
	r.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(200, strings.Repeat("a", (10<<20)+1)), nil
	})
	var out any
	if r.get(context.Background(), "https://www.youtube.com/caption", "", &out) == nil {
		t.Fatal("unbounded response")
	}
	for _, raw := range []string{"https://youtube.com.evil/caption", "https://u@youtube.com/caption", "https://youtube.com:8000/caption", "http://youtube.com/caption"} {
		if _, err := safeCaption(raw, false); err == nil {
			t.Fatal(raw)
		}
	}
	b := &limitedBuffer{limit: 5}
	if _, err := b.Write([]byte("123456")); err == nil {
		t.Fatal("unbounded CLI")
	}
}
func TestAutomaticLanguagePriority(t *testing.T) {
	lang, _ := chooseCaption(map[string][]captionFormat{"zh-Hans": {{Ext: "json3", URL: "zh"}}, "en-orig": {{Ext: "json3", URL: "en"}}}, "en", true)
	if lang != "en-orig" {
		t.Fatal(lang)
	}
}
