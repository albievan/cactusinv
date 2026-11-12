//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// restart execs exe in place of this process; it returns only on failure.
func restart(exe string) error {
	return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), os.Environ()) //nolint:gosec // the binary just installed
}
