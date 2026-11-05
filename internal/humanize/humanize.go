// Package humanize formats machine values for humans. It's a small local home
// for cross-package helpers that don't (yet) belong in a shared amberpixels
// library.
package humanize

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

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

// Duration renders d in its largest fitting unit with at most one decimal, e.g.
// "45 s", "2.5 min", "1.2 h". Each unit takes over once a value would round up
// to it, so 59.97 s reads "1 min", never "60 s". Negative durations read as 0.
func Duration(d time.Duration) string {
	secs := math.Round(max(d, 0).Seconds())
	if secs < 60 {
		return strconv.Itoa(int(secs)) + " s"
	}
	if mins := round1(d.Minutes()); mins < 60 {
		return trimZero(mins) + " min"
	}
	return trimZero(round1(d.Hours())) + " h"
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// trimZero formats v with one decimal and drops a trailing ".0".
func trimZero(v float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
}
