package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const maxLogSize = 10 << 20

// openLogFile opens the server log for appending: GARMIN_MCP_LOG_FILE, or <base>/garmin-mcp.log.
// A file over maxLogSize is moved to <path>.1 first, so at most two files are kept.
// shortcut: size is checked only at startup; rotate while running if long sessions outgrow it.
func openLogFile(base string, getenv func(string) string) (*os.File, error) {
	path := getenv("GARMIN_MCP_LOG_FILE")
	if path == "" {
		path = filepath.Join(base, "garmin-mcp.log")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		os.Rename(path, path+".1") // best effort: fails on Windows while another server has it open
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// stampWriter prefixes every write with the pid and local time, so lines from several servers sharing the
// file (one per MCP client) can be told apart. Callers write whole lines.
type stampWriter struct{ l *log.Logger }

func newStampWriter(f *os.File) stampWriter {
	return stampWriter{log.New(f, fmt.Sprintf("pid=%d ", os.Getpid()), log.LstdFlags|log.Lmicroseconds|log.Lmsgprefix)}
}

func (w stampWriter) Write(p []byte) (int, error) {
	if err := w.l.Output(2, string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}
