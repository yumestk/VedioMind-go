package platform

import (
	"context"
	"os"
	"testing"
	"time"
)

// Run explicitly with LIVE_VIDEO_URL; ordinary tests never access real platforms.
func TestLivePlatform(t *testing.T) {
	raw := os.Getenv("LIVE_VIDEO_URL")
	if raw == "" {
		t.Skip("explicit live platform smoke only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r := New("", os.Getenv("YTDLP_PROXY"))
	target, err := r.Normalize(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	v, err := r.Resolve(ctx, target)
	if err != nil {
		t.Fatalf("live platform %s %s: %v", target.Platform, target.URL, err)
	}
	t.Logf("platform=%s title=%q language=%s source=%s segments=%d canonical=%s", v.Platform, v.Title, v.Language, v.Source, len(v.Segments), v.CanonicalURL)
}
