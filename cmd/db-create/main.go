package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/drypa/book-shelf/book"
	"github.com/drypa/book-shelf/internal/config"
	"github.com/drypa/book-shelf/internal/logging"
	"github.com/drypa/book-shelf/scanner"
	_ "github.com/mattn/go-sqlite3"
)

// Exit codes (ADR §5.7): 2 for wrong command line arguments, 1 for a fatal
// failure. ./db-create.sh propagates them to the operator unchanged.
const (
	exitFailure = 1
	exitUsage   = 2
)

// errUsage marks a wrong command line; it is already logged when it is returned.
var errUsage = errors.New("usage: db-create <library-dir>")

func main() {
	logging.Setup()

	err := run(os.Args[1:])
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(exitUsage)
	default:
		slog.Error("db-create failed", "err", err)
		os.Exit(exitFailure)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		slog.Error(errUsage.Error(),
			"hint", "the directory comes from command: of the db-create service in docker-compose.yml "+
				"(LIBRARY_CONTAINER_DIR); run ./db-create.sh instead of calling the binary directly")
		return errUsage
	}
	libraryDir := args[0]

	// The database path is configured, not derived from the working directory.
	values, err := config.Required("DB_DSN")
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	dsn := values["DB_DSN"]

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("close database", "err", closeErr)
		}
	}()
	// sql.Open does not touch the filesystem: without this check an unusable
	// path would only show up as a confusing error on the first statement.
	if err := db.Ping(); err != nil {
		return fmt.Errorf("open database %q: %w (check DATA_HOST_DIR, DATA_CONTAINER_DIR and DB_FILE in .env)", dsn, err)
	}

	if err := createTable(db); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	files, err := scanner.GetFilesByMask(libraryDir, ".zip.json")
	if err != nil {
		return fmt.Errorf("read library directory %q: %w", libraryDir, err)
	}
	// A wrong LIBRARY_HOST_DIR leaves an empty directory behind (ADR E1/K1), so
	// an empty result means "not scanned yet", not "nothing to do".
	if len(files) == 0 {
		return fmt.Errorf("no *.zip.json sidecar files found in %q, run ./scan.sh first "+
			"and check LIBRARY_HOST_DIR in .env", libraryDir)
	}

	for _, path := range files {
		slog.Info("processing sidecar", "path", path)
		base := filepath.Base(path)
		zipName := strings.TrimSuffix(base, filepath.Ext(base))
		infos, err := readJson(path)
		if err != nil {
			return fmt.Errorf("read sidecar %q: %w", path, err)
		}
		if err := insertBooks(db, infos, zipName); err != nil {
			return fmt.Errorf("insert books from %q: %w", path, err)
		}
	}

	slog.Info("db-create finished", "sidecars", len(files), "dsn", dsn)
	return nil
}

func insertBooks(db *sql.DB, infos []*book.Info, zipName string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	insertQuery := `INSERT INTO books (title, authors,annotation,genre,keywords,archive,file_name,file_size)
	Values (?,?,?,?,?,?,?,?)`
	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range infos {
		if b != nil {
			if b.Description == nil || b.TitleInfo == nil {
				slog.Warn("book has no title info", "file", b.Filename, "archive", zipName)
				continue
			}
			_, err := stmt.Exec(
				b.TitleInfo.BookTitle,
				b.TitleInfo.Authors(),
				b.Annotation,
				b.Genre,
				b.Keywords,
				zipName,
				b.Filename,
				b.SizeInBytes,
			)

			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func readJson(path string) ([]*book.Info, error) {
	fileData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data []*book.Info
	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func createTable(db *sql.DB) error {
	createTableSQL := `CREATE TABLE IF NOT EXISTS books (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT,
    authors TEXT,
    annotation TEXT,
    genre TEXT,
    keywords TEXT,
    archive TEXT,
    file_name TEXT,
    file_size BIGINT)`
	_, err := db.Exec(createTableSQL)
	return err
}
