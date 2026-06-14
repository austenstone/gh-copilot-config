//go:build darwin

package profile

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns the creation time of path on macOS, or zero if unavailable.
func birthTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	}
	return time.Time{}
}
