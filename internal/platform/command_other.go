//go:build !linux && !darwin

package platform

import "os/exec"

// Container deployments target Linux; other systems keep CommandContext's default cancellation.
func prepareCommand(cmd *exec.Cmd) {}
