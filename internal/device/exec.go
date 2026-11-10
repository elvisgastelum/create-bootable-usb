package device

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// run executes a system tool and returns its standard output. On failure
// the error carries the tool's own message so users can act on it.
func run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		if msg != "" {
			return out, fmt.Errorf("%s failed: %w: %s", name, err, msg)
		}
		return out, fmt.Errorf("%s failed: %w", name, err)
	}
	return out, nil
}
