// Package logging sets up structured logging for all three binaries.
//
// slog writes records to stdout in JSON (or text) form, so every message is
// visible through `docker compose logs` and nothing is ever written to a file.
// The standard logger, which scanner/scan.go still uses, is redirected into slog
// via slog.NewLogLogger - existing log.Printf calls therefore become structured
// records without touching business code.
package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/drypa/book-shelf/internal/config"
)

// Environment variables understood by Setup.
const (
	levelKey  = "LOG_LEVEL"
	formatKey = "LOG_FORMAT"
)

// Setup configures the default slog logger and redirects the standard logger
// into it. It never fails: an unknown LOG_LEVEL or LOG_FORMAT is reported as a
// warning and the defaults are used instead.
func Setup() {
	level, levelWarning := parseLevel(config.Lookup(levelKey))
	format, formatWarning := parseFormat(config.Lookup(formatKey))

	handler := newHandler(os.Stdout, format, level)
	slog.SetDefault(slog.New(handler))

	log.SetFlags(0)
	// log.Logger.Writer() forwards every write to slog at the given level.
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())

	if levelWarning != "" {
		slog.Warn(levelWarning, "key", levelKey, "using", level.String())
	}
	if formatWarning != "" {
		slog.Warn(formatWarning, "key", formatKey, "using", format)
	}
}

func newHandler(w io.Writer, format string, level slog.Level) slog.Handler {
	options := &slog.HandlerOptions{Level: level}
	if format == "text" {
		return slog.NewTextHandler(w, options)
	}
	return slog.NewJSONHandler(w, options)
}

func parseLevel(value string) (slog.Level, string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return slog.LevelInfo, ""
	case "debug":
		return slog.LevelDebug, ""
	case "info":
		return slog.LevelInfo, ""
	case "warn":
		return slog.LevelWarn, ""
	case "error":
		return slog.LevelError, ""
	default:
		return slog.LevelInfo, "unknown log level, using info"
	}
}

func parseFormat(value string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "json", ""
	case "json":
		return "json", ""
	case "text":
		return "text", ""
	default:
		return "json", "unknown log format, using json"
	}
}
