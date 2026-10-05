package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/drypa/book-shelf/internal/config"
	"github.com/drypa/book-shelf/internal/logging"
	"github.com/drypa/book-shelf/scanner"
)

const ScanParallelismKey = "SCAN_PARALLELISM"
const defaultParallelism = 5

// Exit codes (ADR §5.7): 2 for wrong command line arguments, 1 for a fatal
// failure of the run. ./scan.sh propagates them to the operator unchanged.
const (
	exitFailure = 1
	exitUsage   = 2
)

// errUsage marks a wrong command line; it is already logged when it is returned.
var errUsage = errors.New("usage: scan <library-dir>")

func main() {
	logging.Setup()

	err := run(os.Args[1:])
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(exitUsage)
	default:
		slog.Error("scan failed", "err", err)
		os.Exit(exitFailure)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		slog.Error(errUsage.Error(),
			"hint", "the directory comes from command: of the scan service in docker-compose.yml "+
				"(LIBRARY_CONTAINER_DIR); run ./scan.sh instead of calling the binary directly")
		return errUsage
	}
	dir := args[0]
	parallelism := resolveParallelism()

	// Counted before the scan itself: a wrong path in LIBRARY_HOST_DIR makes
	// docker create an empty directory, which would otherwise look like a
	// successful run over an empty library (ADR K1).
	archives, err := scanner.GetFilesByMask(dir, ".zip")
	if err != nil {
		return fmt.Errorf("read library directory %q: %w", dir, err)
	}
	if len(archives) == 0 {
		return fmt.Errorf("no *.zip archives found in %q, check LIBRARY_HOST_DIR in .env", dir)
	}

	if err := scanner.NewScanner(dir, parallelism).Scan(); err != nil {
		// Per-archive failures are logged inside Scan and do not abort the pass;
		// only a failure of the pass itself lands here.
		return fmt.Errorf("scan %q: %w", dir, err)
	}
	slog.Info("scan finished", "dir", dir, "archives", len(archives), "parallelism", parallelism)
	return nil
}

// resolveParallelism keeps the previous semantics - an unparsable value is
// ignored and the default of 5 is used - and additionally rejects values that
// would stall the semaphore inside scanner.
func resolveParallelism() int {
	raw := config.Lookup(ScanParallelismKey)
	if raw == "" {
		return defaultParallelism
	}
	parallelism, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("ignoring non-numeric value, using default",
			ScanParallelismKey, raw, "default", defaultParallelism)
		return defaultParallelism
	}
	if parallelism < 1 {
		slog.Warn("value must be greater than zero, using default",
			ScanParallelismKey, parallelism, "default", defaultParallelism)
		return defaultParallelism
	}
	return parallelism
}
