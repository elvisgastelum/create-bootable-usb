//go:build !windows

package privilege

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// IsElevated reports whether the process runs as root.
func IsElevated() bool { return os.Geteuid() == 0 }

// Elevate replaces the current process with `sudo <program> <args...>`,
// which asks for the password in the terminal. It only returns on failure.
func Elevate(args []string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return errors.New("sudo is not installed; run this program as root")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argv := append([]string{"sudo", "--", exe}, args...)
	return syscall.Exec(sudo, argv, os.Environ())
}
