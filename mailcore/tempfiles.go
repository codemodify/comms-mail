package mailcore

import (
	"os"
	"path/filepath"
	"time"
)

// Temporary copies comms-mail hands to other programs — an attachment
// opened in its application, a page printed from the browser, a file
// dragged out to another window — cannot be removed at once: the other
// program reads them when it gets round to it. They are removed once old
// instead, the next time comms-mail makes one (and when it starts).

// Ages after which the copies go.
const (
	OpenedKeep  = 24 * time.Hour // attachments opened in another application
	PrintedKeep = time.Hour      // pages handed to the browser to print
	DraggedKeep = 24 * time.Hour // attachments dragged out of the window
)

// RemoveOld deletes what matches pattern (files, or directories with their
// contents) last changed more than age ago, and returns how many went. A
// link is removed, never what it points to.
func RemoveOld(pattern string, age time.Duration) int {
	matches, _ := filepath.Glob(pattern)
	cutoff := time.Now().Add(-age)
	n := 0
	for _, m := range matches {
		fi, err := os.Lstat(m)
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		if os.RemoveAll(m) == nil {
			n++
		}
	}
	return n
}
