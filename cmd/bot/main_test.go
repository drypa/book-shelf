package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var botEnv = []string{"BOT_TOKEN", "LIBRARY_DIR", "DB_DSN", "DB_PATH", "DB_CONNECTION_STRING", "HTTP_PROXY"}

func cleanBotEnv(t *testing.T) {
	t.Helper()
	for _, name := range botEnv {
		name := name
		if value, ok := os.LookupEnv(name); ok {
			t.Cleanup(func() { _ = os.Setenv(name, value) })
		}
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf
}

// TestRunReportsEveryMissingVariable is the UC5 contract of the bot: no silent
// success, one message naming all missing variables.
func TestRunReportsEveryMissingVariable(t *testing.T) {
	cleanBotEnv(t)

	err := run()
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	want := "missing required environment variables: BOT_TOKEN, LIBRARY_DIR, DB_DSN"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("run() error = %q, want it to contain %q", err, want)
	}
}

func TestRunReportsSingleMissingVariable(t *testing.T) {
	cleanBotEnv(t)
	libraryDir := t.TempDir()
	t.Setenv("LIBRARY_DIR", libraryDir)
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "db.sqlite3"))
	t.Setenv("BOT_TOKEN", "")

	err := run()
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	want := "missing required environment variables: BOT_TOKEN"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("run() error = %q, want it to contain %q", err, want)
	}
}

func TestRunFailsOnMissingLibraryDirectory(t *testing.T) {
	cleanBotEnv(t)
	missing := filepath.Join(t.TempDir(), "no-such-library")
	t.Setenv("BOT_TOKEN", "test-token")
	t.Setenv("LIBRARY_DIR", missing)
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "db.sqlite3"))

	err := run()
	if err == nil || !strings.Contains(err.Error(), "init storage") {
		t.Fatalf("run() error = %v, want an init storage error", err)
	}
}

func TestWarnIfLibraryIsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantLog string
	}{
		{name: "empty directory", files: nil, wantLog: "no *.zip archives"},
		{name: "directory with unrelated files", files: []string{"readme.txt"}, wantLog: "no *.zip archives"},
		{name: "directory with an archive", files: []string{"book.zip"}, wantLog: ""},
		{name: "directory with a sidecar only", files: []string{"book.zip.json"}, wantLog: "no *.zip archives"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			logs := captureLogs(t)

			warnIfLibraryIsEmpty(dir)

			switch {
			case tc.wantLog == "" && logs.Len() > 0:
				t.Fatalf("unexpected warning: %s", logs.String())
			case tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog):
				t.Fatalf("logs %q do not contain %q", logs.String(), tc.wantLog)
			}
		})
	}
}

func TestWaitForShutdown(t *testing.T) {
	logs := captureLogs(t)

	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "db.sqlite3"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- waitForShutdown(ctx, db) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitForShutdown() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitForShutdown() did not return after context cancellation")
	}

	if !strings.Contains(logs.String(), "shutdown complete") {
		t.Fatalf("logs %q do not contain %q", logs.String(), "shutdown complete")
	}
	// The database must be closed by the shutdown path, so a further query fails.
	if err := db.Ping(); err == nil {
		t.Fatal("database is still open after shutdown")
	}
}
