// Package format renders sizes and durations for people.
package format

import (
	"fmt"
	"time"
)

// Bytes formats n with decimal (SI) units, the way drive vendors and macOS
// label capacities: 16043212800 becomes "16.0 GB".
func Bytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Rate formats a transfer speed in bytes per second.
func Rate(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "—"
	}
	return Bytes(int64(bytesPerSecond)) + "/s"
}

// Duration rounds d to whole seconds: "1m12s".
func Duration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}
