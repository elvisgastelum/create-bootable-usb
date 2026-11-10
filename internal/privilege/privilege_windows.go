//go:build windows

package privilege

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// IsElevated reports whether the process runs with an elevated
// (Administrator) token.
func IsElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// Elevate starts the program again through the UAC "runas" prompt in a new
// console window and returns ErrRelaunched on success.
func Elevate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	args := append(append([]string(nil), os.Args[1:]...), ElevatedFlag)
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, params, dir, windows.SW_NORMAL); err != nil {
		return fmt.Errorf("could not get Administrator rights: %w", err)
	}
	return ErrRelaunched
}
