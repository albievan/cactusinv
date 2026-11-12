//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
)

// restart starts exe with this process's arguments, standard streams and environment;
// on success this process exits with exe's exit code once it ends, so a console or a
// service wrapper still sees one program.
func restart(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...) //nolint:gosec // the binary just installed
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	os.Exit(cmd.ProcessState.ExitCode())
	return nil
}
