// Package config reads application configuration from the process environment.
//
// Rules:
//
//   - a missing required variable is an error, and the caller turns it into a
//     non-zero exit code instead of a silent success;
//   - all missing variables are reported by a single error, not one by one;
//   - renamed variables fall back to their deprecated name with a warning.
//
// Values are read from the container environment only. The `.env` file is read
// by docker compose, not by this package.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// LegacyEnv holds the single source of truth for the variable renames: current
// name -> deprecated name that is still honoured as a fallback.
//
// The map lives here instead of in the calling binaries so that the priority
// rule cannot drift apart between them. Removal of the fallbacks is tracked by
// the plan task T-16.
var LegacyEnv = map[string]string{
	"LIBRARY_DIR": "DB_PATH",
	"DB_DSN":      "DB_CONNECTION_STRING",
}

// warned keeps deprecated names already reported, so a fallback produces exactly
// one WARN per process no matter how many times a variable is read.
var warned sync.Map

// Lookup returns the value of an optional variable. Unset and blank-only values
// are both reported as ""; values are trimmed of surrounding whitespace so that
// a token pasted with a trailing newline still works.
func Lookup(name string) string {
	value, _ := lookup(name)
	return value
}

// Get returns the value of the primary variable.
//
// The deprecated name has the lowest priority:
//
//   - the primary variable is set -> it wins, the deprecated one is ignored
//     silently (the operator has already adopted the new name);
//   - the primary one is not set but the deprecated one is -> it is used and a
//     WARN is logged once;
//   - neither is set -> "".
//
// legacy may be empty: in that case the deprecated name is taken from LegacyEnv.
func Get(primary, legacy string) string {
	value, _ := resolve(primary, legacy)
	return value
}

// Required returns the values of all named variables and an error listing every
// variable that is missing, e.g.
//
//	missing required environment variables: BOT_TOKEN, DB_DSN
//
// A deprecated name is accepted here as well, with the same WARN as in Get.
// The returned map is keyed by the requested names and is nil when the error is
// not nil.
func Required(names ...string) (map[string]string, error) {
	values := make(map[string]string, len(names))
	missing := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))

	for _, name := range names {
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}

		value, ok := resolve(name, "")
		if !ok {
			missing = append(missing, name)
			continue
		}
		values[name] = value
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return values, nil
}

// resolve implements the primary -> legacy priority rule shared by Get and Required.
func resolve(name, legacy string) (string, bool) {
	if value, ok := lookup(name); ok {
		return value, true
	}
	if legacy == "" {
		legacy = LegacyEnv[name]
	}
	if legacy == "" {
		return "", false
	}
	value, ok := lookup(legacy)
	if ok {
		warnDeprecated(name, legacy)
	}
	return value, ok
}

// lookup reports the trimmed value of name and whether it can be used. A
// variable that is unset, empty or whitespace-only counts as not set, so that
// blank entries in .env fail the same way as missing ones.
func lookup(name string) (string, bool) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return "", false
	}
	value := strings.TrimSpace(raw)
	return value, value != ""
}

func warnDeprecated(current, deprecated string) {
	if _, reported := warned.LoadOrStore(deprecated, struct{}{}); reported {
		return
	}
	slog.Warn(
		fmt.Sprintf("DEPRECATED: %s is deprecated, use %s", deprecated, current),
		"deprecated", deprecated,
		"replacement", current,
	)
}

// resetWarnings clears the "already warned" state. Test-only helper.
func resetWarnings() {
	warned.Clear()
}
