//go:build linux || darwin

package platform

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestTimeoutKillsChildRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	prepareCommand(cmd)
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	_, err := cmd.Output()
	if err == nil || ctx.Err() == nil {
		t.Fatal("expected cancellation", err)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("child kept stdout open after parent cancellation")
	}
}
