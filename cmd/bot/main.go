package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/drypa/book-shelf/internal/config"
	"github.com/drypa/book-shelf/internal/logging"
	"github.com/drypa/book-shelf/scanner"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	logging.Setup()
	if err := run(); err != nil {
		slog.Error("bot failed", "err", err)
		os.Exit(1)
	}
}

// run is the single place where the bot can fail: every configuration problem
// becomes an error here and therefore a non-zero exit code in main, instead of
// the silent return the previous version used.
func run() error {
	// Configuration is read before any side effect, so a missing variable is
	// reported before the storage is probed.
	values, err := config.Required("BOT_TOKEN", "LIBRARY_DIR", "DB_DSN")
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	botToken := values["BOT_TOKEN"]
	libraryDir := values["LIBRARY_DIR"]
	dsn := values["DB_DSN"]
	// Optional; an empty value means a direct connection, not an invalid proxy.
	httpProxy := config.Lookup("HTTP_PROXY")

	storage, err := newStorage(libraryDir)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	warnIfLibraryIsEmpty(libraryDir)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	// sql.Open is lazy: without this check an unusable database path would only
	// surface much later, on the first user search.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return fmt.Errorf("open database %q: %w", dsn, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repository := NewRepository(db)
	if _, err := newBot(ctx, botToken, repository, storage, httpProxy); err != nil {
		_ = db.Close()
		return fmt.Errorf("start bot: %w", err)
	}
	slog.Info("bot started", "library", libraryDir, "dsn", dsn, "proxy", httpProxy != "")

	if err := waitForShutdown(ctx, db); err != nil {
		return err
	}
	return nil
}

// waitForShutdown blocks until SIGTERM/SIGINT, then closes the database in a
// defined order: context cancellation (which stops processNotifications) ->
// database close -> final log line.
func waitForShutdown(ctx context.Context, db *sql.DB) error {
	<-ctx.Done()
	slog.Info("shutting down")
	if err := db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	slog.Info("shutdown complete")
	return nil
}

// warnIfLibraryIsEmpty catches the most likely first-deploy mistake: a typo in
// LIBRARY_HOST_DIR makes docker create an empty directory owned by root (ADR
// E1/K1), and the bot would then quietly find nothing to serve.
func warnIfLibraryIsEmpty(libraryDir string) {
	archives, err := scanner.GetFilesByMask(libraryDir, ".zip")
	if err != nil {
		slog.Warn("cannot list library archives", "dir", libraryDir, "err", err)
		return
	}
	if len(archives) == 0 {
		slog.Warn("library directory contains no *.zip archives, searches will return nothing",
			"dir", libraryDir,
			"hint", "check LIBRARY_HOST_DIR in .env and run ./scan.sh",
		)
	}
}
