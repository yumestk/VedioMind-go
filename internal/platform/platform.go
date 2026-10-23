package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                   { return e.Code }
func problem(status int, code, msg string) error { return &Error{status, code, msg} }
func unavailable() error {
	return problem(502, "EXTERNAL_PLATFORM_UNAVAILABLE", "平台暂时无法提供有效字幕，请稍后再试")
}
func noSubtitle() error {
	return problem(422, "SUBTITLE_NOT_AVAILABLE", "该视频暂无可用平台字幕")
}
func invalid() error {
	return problem(400, "INVALID_VIDEO_URL", "请输入有效的 Bilibili / YouTube 单视频链接")
}
func mapError(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	var n net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &n) && n.Timeout()) {
		return problem(504, "EXTERNAL_RESOLVE_TIMEOUT", "读取平台字幕超时，请稍后重试")
	}
	return unavailable()
}

type Segment struct {
	ID      int    `json:"id"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
}
type Video struct {
	Platform     string    `json:"platform"`
	ExternalID   string    `json:"externalId"`
	CanonicalURL string    `json:"canonicalUrl"`
	Title        string    `json:"title"`
	DurationMs   int64     `json:"durationMs"`
	Source       string    `json:"source"`
	Language     string    `json:"language"`
	Segments     []Segment `json:"-"`
}
type Target struct{ Platform, ID, URL string }
type Resolver struct {
	Client  *http.Client
	Inspect func(context.Context, string) ([]byte, error)
}

func New(binary, proxy string) *Resolver {
	if binary == "" {
		binary = "yt-dlp"
	}
	return &Resolver{Client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Inspect: func(ctx context.Context, canonical string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		args := []string{"--ignore-config", "--no-update", "--js-runtimes", "node", "--dump-single-json", "--skip-download", "--no-playlist", "--socket-timeout", "20", "--retries", "1"}
		if proxy != "" {
			args = append(args, "--proxy", proxy)
		}
		args = append(args, canonical)
		cmd := exec.CommandContext(ctx, binary, args...)
		prepareCommand(cmd)
		cmd.WaitDelay = 2 * time.Second
		out := &limitedBuffer{limit: 20 << 20}
		stderr := &limitedBuffer{limit: 4096}
		cmd.Stdout = out
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, mapError(ctx.Err())
			}
			return nil, unavailable()
		}
		return out.Bytes(), nil
	}}
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("output exceeds limit")
	}
	return b.Buffer.Write(p)
}

var biliID = regexp.MustCompile(`^/video/(BV[0-9A-Za-z]{10})(?:/|$)`)
var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func validURL(raw string) (*url.URL, error) {
	if len(raw) > 2000 {
		return nil, invalid()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" {
		return nil, invalid()
	}
	if u.Port() != "" && !((u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) {
		return nil, invalid()
	}
	return u, nil
}
func isBili(host string) bool {
	return host == "bilibili.com" || host == "www.bilibili.com" || host == "m.bilibili.com" || host == "b23.tv"
}
func isYoutube(host string) bool {
	return host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" || host == "youtu.be"
}
func (r *Resolver) Normalize(ctx context.Context, raw string) (Target, error) {
	u, err := validURL(raw)
	if err != nil {
		return Target{}, err
	}
	host := strings.ToLower(u.Hostname())
	if isBili(host) {
		for i := 0; host == "b23.tv"; i++ {
			if i >= 3 {
				return Target{}, invalid()
			}
			req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
			if e != nil {
				return Target{}, invalid()
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 VedioMind/1.0")
			res, e := r.Client.Do(req)
			if e != nil {
				return Target{}, mapError(e)
			}
			res.Body.Close()
			if res.StatusCode == 429 {
				return Target{}, problem(429, "EXTERNAL_PLATFORM_RATE_LIMITED", "平台请求过于频繁，请稍后重试")
			}
			if res.StatusCode < 300 || res.StatusCode > 399 {
				return Target{}, invalid()
			}
			next, e := res.Location()
			if e != nil {
				return Target{}, invalid()
			}
			u, e = validURL(next.String())
			if e != nil || !isBili(strings.ToLower(u.Hostname())) {
				return Target{}, invalid()
			}
			host = strings.ToLower(u.Hostname())
		}
		match := biliID.FindStringSubmatch(u.Path)
		if match == nil {
			return Target{}, invalid()
		}
		return Target{"BILIBILI", match[1], "https://www.bilibili.com/video/" + match[1]}, nil
	}
	if isYoutube(host) {
		var id string
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if host == "youtu.be" && len(parts) == 1 {
			id = parts[0]
		} else if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed") {
			id = parts[1]
		}
		if !youtubeID.MatchString(id) {
			return Target{}, invalid()
		}
		return Target{"YOUTUBE", id, "https://www.youtube.com/watch?v=" + id}, nil
	}
	return Target{}, problem(400, "UNSUPPORTED_PLATFORM", "目前仅支持 Bilibili 和 YouTube")
}
func (r *Resolver) get(ctx context.Context, raw, referer string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return unavailable()
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 VedioMind/1.0")
	req.Header.Set("Referer", referer)
	res, err := r.Client.Do(req)
	if err != nil {
		return mapError(err)
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return problem(429, "EXTERNAL_PLATFORM_RATE_LIMITED", "平台请求过于频繁，请稍后重试")
	}
	if res.StatusCode != 200 {
		return unavailable()
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (10<<20)+1))
	if err != nil {
		return mapError(err)
	}
	if len(b) > 10<<20 {
		return unavailable()
	}
	if json.Unmarshal(b, v) != nil {
		return unavailable()
	}
	return nil
}
func (r *Resolver) Resolve(ctx context.Context, t Target) (Video, error) {
	if t.Platform == "BILIBILI" {
		return r.bilibili(ctx, t)
	}
	return r.youtube(ctx, t)
}
func safeCaption(raw string, bili bool) (string, error) {
	if bili {
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		}
		raw = strings.Replace(raw, "http://", "https://", 1)
	}
	u, err := validURL(raw)
	if err != nil || u.Scheme != "https" {
		return "", unavailable()
	}
	host := strings.ToLower(u.Hostname())
	if (bili && host != "aisubtitle.hdslb.com") || (!bili && host != "youtube.com" && host != "www.youtube.com") {
		return "", unavailable()
	}
	return u.String(), nil
}
func milliseconds(seconds float64) (int64, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 7*24*3600 {
		return 0, unavailable()
	}
	return max(0, int64(math.Round(seconds*1000))), nil
}
func finish(v Video) (Video, error) {
	if strings.TrimSpace(v.Title) == "" {
		return Video{}, unavailable()
	}
	if len(v.Segments) == 0 {
		return Video{}, noSubtitle()
	}
	if len(v.Segments) > 20000 {
		return Video{}, unavailable()
	}
	for i := range v.Segments {
		v.Segments[i].ID = i + 1
	}
	return v, nil
}
func (r *Resolver) bilibili(ctx context.Context, t Target) (Video, error) {
	var metadata struct {
		Code int `json:"code"`
		Data struct {
			Aid      int64   `json:"aid"`
			Cid      int64   `json:"cid"`
			Title    string  `json:"title"`
			Duration float64 `json:"duration"`
		} `json:"data"`
	}
	if err := r.get(ctx, "https://api.bilibili.com/x/web-interface/view?bvid="+t.ID, t.URL, &metadata); err != nil {
		return Video{}, err
	}
	m := metadata.Data
	if metadata.Code != 0 || m.Aid <= 0 || m.Cid <= 0 {
		return Video{}, unavailable()
	}
	type caption struct {
		Lan  string `json:"lan"`
		Type int    `json:"type"`
		URL  string `json:"subtitle_url"`
	}
	var list struct {
		Code int `json:"code"`
		Data struct {
			Subtitle struct {
				Subtitles []caption `json:"subtitles"`
			} `json:"subtitle"`
		} `json:"data"`
	}
	endpoint := fmt.Sprintf("https://api.bilibili.com/x/v2/dm/view?aid=%d&oid=%d&type=1", m.Aid, m.Cid)
	if err := r.get(ctx, endpoint, t.URL, &list); err != nil {
		return Video{}, err
	}
	if list.Code != 0 {
		return Video{}, unavailable()
	}
	items := list.Data.Subtitle.Subtitles
	if len(items) == 0 {
		return Video{}, noSubtitle()
	}
	rank := func(c caption) int {
		ai := strings.HasPrefix(c.Lan, "ai-") || c.Type == 1
		zh := strings.Contains(strings.ToLower(c.Lan), "zh")
		n := 0
		if ai {
			n += 2
		}
		if !zh {
			n++
		}
		return n
	}
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
	selected := items[0]
	captionURL, err := safeCaption(selected.URL, true)
	if err != nil {
		return Video{}, err
	}
	var body struct {
		Body []struct {
			From    float64 `json:"from"`
			To      float64 `json:"to"`
			Content string  `json:"content"`
		} `json:"body"`
	}
	if err = r.get(ctx, captionURL, t.URL, &body); err != nil {
		return Video{}, err
	}
	duration, err := milliseconds(m.Duration)
	if err != nil {
		return Video{}, err
	}
	v := Video{Platform: t.Platform, ExternalID: t.ID, CanonicalURL: t.URL, Title: strings.TrimSpace(m.Title), DurationMs: duration, Language: selected.Lan, Source: "BILIBILI_MANUAL", Segments: []Segment{}}
	if selected.Type == 1 || strings.HasPrefix(selected.Lan, "ai-") {
		v.Source = "BILIBILI_AI"
	}
	for _, row := range body.Body {
		content := strings.TrimSpace(row.Content)
		if content == "" {
			continue
		}
		start, err := milliseconds(row.From)
		if err != nil {
			return Video{}, err
		}
		end, err := milliseconds(row.To)
		if err != nil {
			return Video{}, err
		}
		v.Segments = append(v.Segments, Segment{StartMs: start, EndMs: max(start, end), Text: content})
	}
	return finish(v)
}

type captionFormat struct {
	Ext string `json:"ext"`
	URL string `json:"url"`
}

func chooseCaption(captions map[string][]captionFormat, original string, automatic bool) (string, string) {
	languages := make([]string, 0, len(captions))
	for lang := range captions {
		languages = append(languages, lang)
	}
	sort.Strings(languages)
	rank := func(lang string) int {
		lang = strings.ToLower(lang)
		base := strings.Split(strings.ToLower(original), "-")[0]
		matches := original != "" && (lang == strings.ToLower(original) || lang == base || lang == base+"-orig")
		if !automatic {
			if strings.HasPrefix(lang, "zh") {
				return 0
			}
			if matches {
				return 1
			}
			return 2
		}
		if strings.HasSuffix(lang, "-orig") {
			if matches {
				return 0
			}
			return 1
		}
		if matches {
			return 2
		}
		if strings.HasPrefix(lang, "zh") {
			return 3
		}
		if strings.HasPrefix(lang, "en") {
			return 4
		}
		return 5
	}
	sort.SliceStable(languages, func(i, j int) bool { return rank(languages[i]) < rank(languages[j]) })
	for _, lang := range languages {
		for _, f := range captions[lang] {
			if f.Ext == "json3" && f.URL != "" {
				return lang, f.URL
			}
		}
	}
	return "", ""
}
func (r *Resolver) youtube(ctx context.Context, t Target) (Video, error) {
	b, err := r.Inspect(ctx, t.URL)
	if err != nil {
		return Video{}, mapError(err)
	}
	if len(b) > 20<<20 {
		return Video{}, unavailable()
	}
	var metadata struct {
		ID        string                     `json:"id"`
		Title     string                     `json:"title"`
		Duration  float64                    `json:"duration"`
		Language  string                     `json:"language"`
		Subtitles map[string][]captionFormat `json:"subtitles"`
		Automatic map[string][]captionFormat `json:"automatic_captions"`
	}
	if json.Unmarshal(b, &metadata) != nil || metadata.ID != t.ID {
		return Video{}, unavailable()
	}
	m := metadata
	lang, raw := chooseCaption(m.Subtitles, m.Language, false)
	auto := false
	if raw == "" {
		lang, raw = chooseCaption(m.Automatic, m.Language, true)
		auto = true
	}
	if raw == "" {
		return Video{}, noSubtitle()
	}
	captionURL, err := safeCaption(raw, false)
	if err != nil {
		return Video{}, err
	}
	var body struct {
		Events []struct {
			Start    int64  `json:"tStartMs"`
			Duration *int64 `json:"dDurationMs"`
			Segs     []struct {
				Text string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if err = r.get(ctx, captionURL, t.URL, &body); err != nil {
		return Video{}, err
	}
	duration, err := milliseconds(m.Duration)
	if err != nil {
		return Video{}, err
	}
	v := Video{Platform: t.Platform, ExternalID: t.ID, CanonicalURL: t.URL, Title: strings.TrimSpace(m.Title), DurationMs: duration, Language: strings.TrimSuffix(lang, "-orig"), Source: "YOUTUBE_MANUAL", Segments: []Segment{}}
	if auto {
		v.Source = "YOUTUBE_AUTO"
	}
	for _, event := range body.Events {
		var text strings.Builder
		for _, part := range event.Segs {
			text.WriteString(part.Text)
		}
		content := strings.Join(strings.Fields(text.String()), " ")
		if content == "" {
			continue
		}
		d := int64(2000)
		if event.Duration != nil {
			d = max(1, *event.Duration)
		}
		start := max(0, event.Start)
		if start > 7*24*3600000 || d > 7*24*3600000 {
			return Video{}, unavailable()
		}
		v.Segments = append(v.Segments, Segment{StartMs: start, EndMs: start + d, Text: content})
	}
	return finish(v)
}
