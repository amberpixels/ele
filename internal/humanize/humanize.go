// Package humanize formats machine values for humans. It's a small local home
// for cross-package helpers that don't (yet) belong in a shared amberpixels
// library.
package humanize

import "fmt"

// Bytes renders a byte count compactly with binary (1024) units, e.g. "3.1 GB".
func Bytes(n int64) string {
	const unit = 1024
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
