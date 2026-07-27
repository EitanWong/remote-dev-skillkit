//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package shelladapter

import "os/exec"

// configureCommandCancellation keeps the platform default until that platform
// has a process-tree cancellation primitive with equivalent semantics.
func configureCommandCancellation(*exec.Cmd) {}
