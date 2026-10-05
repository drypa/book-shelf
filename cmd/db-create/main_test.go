package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

const sidecarFixture = `[{"title-info":{"book-title":"Fixture Book","author":[{"firstname":"Ann","lastname":"Smith"}]},` +
	`"genre":"sf","annotation":"About","keywords":"a,b","size_in_bytes":42,"filename":"1.fb2"}]`

func writeSidecar(t *testing.T, library, archive string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(library, archive+".zip.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func TestRunWithoutArguments(t *testing.T) {
	t.Setenv("DB_DSN", "")

	err := run(nil)
	if !errors.Is(err, errUsage) {
		t.Fatalf("run() error = %v, want errUsage", err)
	}
}

func TestRunWithoutDSN(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("DB_CONNECTION_STRING", "")

	err := run([]string{t.TempDir()})
	if err == nil {
		t.Fatal("run() error = nil, want a configuration error")
	}
	want := "missing required environment variables: DB_DSN"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("run() error = %q, want it to contain %q", err, want)
	}
}

func TestRunWithUnusableDatabasePath(t *testing.T) {
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "no-such-dir", "db.sqlite3"))

	err := run([]string{t.TempDir()})
	if err == nil {
		t.Fatal("run() error = nil, want an open error")
	}
	if !strings.Contains(err.Error(), "DATA_HOST_DIR") {
		t.Fatalf("run() error = %q, want a hint about the database paths", err)
	}
}

func TestRunWithoutSidecars(t *testing.T) {
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "db.sqlite3"))

	err := run([]string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "run ./scan.sh first") {
		t.Fatalf("run() error = %v, want a hint to run the scanner first", err)
	}
}

func TestRunFillsDatabase(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "db.sqlite3")
	t.Setenv("DB_DSN", dsn)
	library := t.TempDir()
	writeSidecar(t, library, "books", sidecarFixture)

	if err := run([]string{library}); err != nil {
		t.Fatalf("run() unexpected error: %v", err)
	}

	rows := queryBooks(t, dsn)
	if len(rows) != 1 {
		t.Fatalf("books = %d, want 1", len(rows))
	}
	if got := rows[0]; got.title != "Fixture Book" || got.archive != "books.zip" || got.fileName != "1.fb2" {
		t.Fatalf("unexpected row: %+v", got)
	}
}

func TestRunAcceptsLegacyDSNName(t *testing.T) {
	t.Setenv("DB_DSN", "")
	dsn := filepath.Join(t.TempDir(), "db.sqlite3")
	t.Setenv("DB_CONNECTION_STRING", dsn)
	library := t.TempDir()
	writeSidecar(t, library, "books", sidecarFixture)

	if err := run([]string{library}); err != nil {
		t.Fatalf("run() unexpected error: %v", err)
	}
	if got := queryBooks(t, dsn); len(got) != 1 {
		t.Fatalf("books = %d, want 1", len(got))
	}
}

func TestRunRejectsBrokenSidecar(t *testing.T) {
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "db.sqlite3"))
	library := t.TempDir()
	writeSidecar(t, library, "books", "{not json")

	err := run([]string{library})
	if err == nil || !strings.Contains(err.Error(), "read sidecar") {
		t.Fatalf("run() error = %v, want a sidecar read error", err)
	}
}

// TestSchemaMatchesRepository guards the table layout shared with
// cmd/bot/repository.go: a silent change here breaks the bot's SELECT.
func TestSchemaMatchesRepository(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "db.sqlite3")
	t.Setenv("DB_DSN", dsn)
	library := t.TempDir()
	writeSidecar(t, library, "books", sidecarFixture)

	if err := run([]string{library}); err != nil {
		t.Fatalf("run() unexpected error: %v", err)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, title, authors, annotation, genre, keywords, archive, file_name, file_size FROM books`)
	if err != nil {
		t.Fatalf("the bot's column list does not match the table: %v", err)
	}
	rows.Close()
}

type bookRow struct {
	title    string
	archive  string
	fileName string
}

func queryBooks(t *testing.T, dsn string) []bookRow {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT title, archive, file_name FROM books`)
	if err != nil {
		t.Fatalf("query books: %v", err)
	}
	defer rows.Close()

	var result []bookRow
	for rows.Next() {
		var row bookRow
		if err := rows.Scan(&row.title, &row.archive, &row.fileName); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
	}
	return result
}
