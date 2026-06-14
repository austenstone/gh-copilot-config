//go:build !darwin

package profile

import (
	"os"
	"time"
)

// birthTime falls back to the modification time on platforms that do not expose
// a portable creation time.
func birthTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
