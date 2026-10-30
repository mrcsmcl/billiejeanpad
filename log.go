package main

import (
	"log"
	"os"
	"path/filepath"
)

var logger *log.Logger

// setupLog opens log.txt in %LOCALAPPDATA%\BillieJeanPad\ for append-only
// writes. Falls back to stderr if the file cannot be opened.
func setupLog() {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), AppName)
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(
		filepath.Join(dir, "log.txt"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
		return
	}
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
}

func logf(format string, args ...any) {
	if logger == nil {
		return
	}
	logger.Printf(format, args...)
}
