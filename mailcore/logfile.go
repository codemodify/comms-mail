package mailcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The daemon's log: what went wrong in daily use, to diagnose it later —
// a connection lost and back, a folder that would not sync, a send that
// failed or waits in the Outbox, an error answered to the window. It never
// holds message content or credentials. It is off until OpenLog, so tests
// and the demo write nothing.

var maxLogBytes int64 = 4 << 20 // then comms-maild.log moves to .log.1

var logState struct {
	mu   sync.Mutex
	f    *os.File
	path string
	size int64
}

// LogPath is where the log is written, or "" when logging is off.
func LogPath() string {
	logState.mu.Lock()
	defer logState.mu.Unlock()
	return logState.path
}

// OpenLog starts logging to dir/comms-maild.log (created 0600).
func OpenLog(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "comms-maild.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	fi, _ := f.Stat()
	logState.mu.Lock()
	defer logState.mu.Unlock()
	if logState.f != nil {
		_ = logState.f.Close()
	}
	logState.f, logState.path = f, p
	if fi != nil {
		logState.size = fi.Size()
	}
	return p, nil
}

// CloseLog stops logging.
func CloseLog() {
	logState.mu.Lock()
	defer logState.mu.Unlock()
	if logState.f != nil {
		_ = logState.f.Close()
	}
	logState.f, logState.path, logState.size = nil, "", 0
}

// Logf writes one line to the log, when it is open.
func Logf(format string, args ...any) {
	logState.mu.Lock()
	defer logState.mu.Unlock()
	if logState.f == nil {
		return
	}
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	msg = strings.ReplaceAll(msg, "\n", " ⏎ ")
	line := time.Now().Format("2006-01-02 15:04:05.000 ") + msg + "\n"
	if logState.size+int64(len(line)) > maxLogBytes {
		rotateLogLocked()
	}
	n, _ := logState.f.WriteString(line)
	logState.size += int64(n)
}

// rotateLogLocked moves the log to .1 (replacing the older one) and starts
// a new one.
func rotateLogLocked() {
	p := logState.path
	_ = logState.f.Close()
	_ = os.Rename(p, p+".1")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		logState.f, logState.path = nil, ""
		return
	}
	logState.f, logState.size = f, 0
}
