package main

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip creates a zip archive at path containing the given entries.
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()

	archive := zip.NewWriter(file)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatalf("add %s to archive: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write %s into archive: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

const fb2Fixture = `<?xml version="1.0" encoding="utf-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<description><title-info><genre>sf</genre>
<author><first-name>Ann</first-name><last-name>Smith</last-name></author>
<book-title>Fixture Book</book-title>
</title-info></description>
<body><section><p>Text.</p></section></body>
</FictionBook>`

func TestRunWithoutArguments(t *testing.T) {
	t.Setenv(ScanParallelismKey, "")

	err := run(nil)
	if !errors.Is(err, errUsage) {
		t.Fatalf("run() error = %v, want errUsage", err)
	}
}

func TestRunOnMissingDirectory(t *testing.T) {
	t.Setenv(ScanParallelismKey, "")

	err := run([]string{filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("run() error = nil, want a read error")
	}
	if errors.Is(err, errUsage) {
		t.Fatal("a missing directory must not be reported as a usage error")
	}
	if !strings.Contains(err.Error(), "read library directory") {
		t.Fatalf("run() error = %q, want it to mention the library directory", err)
	}
}

func TestRunOnDirectoryWithoutArchives(t *testing.T) {
	t.Setenv(ScanParallelismKey, "")

	err := run([]string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no *.zip archives found") {
		t.Fatalf("run() error = %v, want a complaint about the empty library", err)
	}
}

func TestRunWritesSidecars(t *testing.T) {
	t.Setenv(ScanParallelismKey, "")
	library := t.TempDir()
	writeZip(t, filepath.Join(library, "books.zip"), map[string]string{"1.fb2": fb2Fixture})

	if err := run([]string{library}); err != nil {
		t.Fatalf("run() unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(library, "books.zip.json")); err != nil {
		t.Fatalf("sidecar was not created: %v", err)
	}
}

// TestRunKeepsBrokenArchivesNonFatal pins the documented pipeline behaviour:
// a broken archive is logged and skipped, the pass still succeeds.
func TestRunKeepsBrokenArchivesNonFatal(t *testing.T) {
	t.Setenv(ScanParallelismKey, "")
	library := t.TempDir()
	if err := os.WriteFile(filepath.Join(library, "broken.zip"), []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("write broken archive: %v", err)
	}

	if err := run([]string{library}); err != nil {
		t.Fatalf("run() error = %v, want nil for a per-archive failure", err)
	}
	if _, err := os.Stat(filepath.Join(library, "broken.zip.json")); !os.IsNotExist(err) {
		t.Fatal("a sidecar was written for a broken archive")
	}
}

func TestResolveParallelism(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{value: "", want: defaultParallelism},
		{value: "abc", want: defaultParallelism},
		{value: "0", want: defaultParallelism},
		{value: "-3", want: defaultParallelism},
		{value: "1", want: 1},
		{value: "12", want: 12},
		{value: " 7 ", want: 7},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(ScanParallelismKey, tc.value)
			if got := resolveParallelism(); got != tc.want {
				t.Fatalf("resolveParallelism(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}
