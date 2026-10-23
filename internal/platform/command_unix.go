//go:build linux || darwin

package platform

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// yt-dlp can spawn a JavaScript runtime. Cancel the entire group on timeout.
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
