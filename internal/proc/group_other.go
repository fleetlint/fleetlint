//go:build !unix

package proc

import "os/exec"

// ownGroup leaves the default: the process is killed on cancel, and
// WaitDelay bounds the wait for anything it started.
func ownGroup(*exec.Cmd) {}
