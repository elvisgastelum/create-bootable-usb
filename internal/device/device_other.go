//go:build !darwin && !linux && !windows

package device

import (
	"errors"
	"os"
)

func list() ([]Device, error)             { return nil, errors.ErrUnsupported }
func prepare(Device) error                { return errors.ErrUnsupported }
func openDevice(Device) (*os.File, error) { return nil, errors.ErrUnsupported }
func flush(*os.File) error                { return errors.ErrUnsupported }
func finish(Device) error                 { return errors.ErrUnsupported }
