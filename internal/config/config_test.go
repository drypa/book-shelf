package config

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// allNames is the full set of variables the package may read, so that a test can
// start from a clean environment even when the developer shell exports them.
var allNames = []string{
	"BOT_TOKEN",
	"LIBRARY_DIR",
	"DB_PATH",
	"DB_DSN",
	"DB_CONNECTION_STRING",
	"HTTP_PROXY",
	"SCAN_PARALLELISM",
}

// cleanEnv removes every variable the package knows about and resets the
// "already warned" state, so each test is independent of the host environment.
func cleanEnv(t *testing.T) {
	t.Helper()
	resetWarnings()
	for _, name := range allNames {
		name := name
		if value, ok := os.LookupEnv(name); ok {
			t.Cleanup(func() { _ = os.Setenv(name, value) })
		}
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

// captureLogs installs a slog handler writing into a buffer and returns it.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf
}

func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantLog string
	}{
		{
			name: "primary set",
			env:  map[string]string{"LIBRARY_DIR": "/library"},
			want: "/library",
		},
		{
			name: "primary wins over legacy",
			env: map[string]string{
				"LIBRARY_DIR": "/library",
				"DB_PATH":     "/legacy",
			},
			want:    "/library",
			wantLog: "", // legacy ignored silently
		},
		{
			name:    "legacy fallback with warning",
			env:     map[string]string{"DB_PATH": "/legacy"},
			want:    "/legacy",
			wantLog: "DEPRECATED: DB_PATH is deprecated, use LIBRARY_DIR",
		},
		{
			name: "blank primary falls back to legacy",
			env: map[string]string{
				"LIBRARY_DIR": "   ",
				"DB_PATH":     "/legacy",
			},
			want:    "/legacy",
			wantLog: "DEPRECATED: DB_PATH is deprecated, use LIBRARY_DIR",
		},
		{
			name: "value is trimmed",
			env:  map[string]string{"LIBRARY_DIR": "  /library \n"},
			want: "/library",
		},
		{
			name: "nothing set",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			logs := captureLogs(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}

			if got := Get("LIBRARY_DIR", ""); got != tc.want {
				t.Fatalf("Get() = %q, want %q", got, tc.want)
			}
			if tc.wantLog == "" && logs.Len() > 0 {
				t.Fatalf("unexpected log output: %s", logs.String())
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("log output %q does not contain %q", logs.String(), tc.wantLog)
			}
		})
	}
}

func TestGetWithExplicitLegacyArgument(t *testing.T) {
	cleanEnv(t)
	logs := captureLogs(t)
	t.Setenv("OLD_NAME", "/legacy")

	if got := Get("NEW_NAME", "OLD_NAME"); got != "/legacy" {
		t.Fatalf("Get() = %q, want %q", got, "/legacy")
	}
	if !strings.Contains(logs.String(), "OLD_NAME is deprecated, use NEW_NAME") {
		t.Fatalf("missing deprecation warning, got %q", logs.String())
	}
}

func TestGetUnknownNameHasNoLegacy(t *testing.T) {
	cleanEnv(t)
	captureLogs(t)
	t.Setenv("UNKNOWN_NAME", "value")

	if got := Get("UNKNOWN_NAME", ""); got != "value" {
		t.Fatalf("Get() = %q, want %q", got, "value")
	}
	if got := Get("ANOTHER_UNKNOWN_NAME", ""); got != "" {
		t.Fatalf("Get() = %q, want empty", got)
	}
}

func TestRequired(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		requested []string
		want      map[string]string
		wantErr   string
		wantLog   string
	}{
		{
			name:      "all set",
			env:       map[string]string{"BOT_TOKEN": "token", "LIBRARY_DIR": "/library", "DB_DSN": "/data/db.sqlite3"},
			requested: []string{"BOT_TOKEN", "LIBRARY_DIR", "DB_DSN"},
			want: map[string]string{
				"BOT_TOKEN":   "token",
				"LIBRARY_DIR": "/library",
				"DB_DSN":      "/data/db.sqlite3",
			},
		},
		{
			name:      "single missing is reported",
			env:       map[string]string{"LIBRARY_DIR": "/library", "DB_DSN": "/data/db.sqlite3"},
			requested: []string{"BOT_TOKEN", "LIBRARY_DIR", "DB_DSN"},
			wantErr:   "missing required environment variables: BOT_TOKEN",
		},
		{
			name:      "all missing are reported at once",
			env:       map[string]string{"LIBRARY_DIR": "/library"},
			requested: []string{"BOT_TOKEN", "DB_DSN"},
			wantErr:   "missing required environment variables: BOT_TOKEN, DB_DSN",
		},
		{
			name:      "blank counts as missing",
			env:       map[string]string{"BOT_TOKEN": "\t \n", "DB_DSN": "/data/db.sqlite3"},
			requested: []string{"BOT_TOKEN", "DB_DSN"},
			wantErr:   "missing required environment variables: BOT_TOKEN",
		},
		{
			name:      "legacy fallback is accepted and warned about",
			env:       map[string]string{"DB_PATH": "/legacy", "DB_CONNECTION_STRING": "/data/legacy.sqlite3"},
			requested: []string{"LIBRARY_DIR", "DB_DSN"},
			want: map[string]string{
				"LIBRARY_DIR": "/legacy",
				"DB_DSN":      "/data/legacy.sqlite3",
			},
			wantLog: "DEPRECATED: DB_PATH is deprecated, use LIBRARY_DIR",
		},
		{
			name:      "duplicates are reported once",
			requested: []string{"BOT_TOKEN", "BOT_TOKEN"},
			wantErr:   "missing required environment variables: BOT_TOKEN",
		},
		{
			name:      "no names requested",
			requested: nil,
			want:      map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			logs := captureLogs(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}

			got, err := Required(tc.requested...)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Required() error = nil, want %q", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("Required() error = %q, want %q", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("Required() values = %v, want nil on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Required() unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Required() = %v, want %v", got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Fatalf("Required()[%q] = %q, want %q", name, got[name], want)
				}
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("log output %q does not contain %q", logs.String(), tc.wantLog)
			}
		})
	}
}

func TestDeprecatedWarningIsReportedOnce(t *testing.T) {
	cleanEnv(t)
	logs := captureLogs(t)
	t.Setenv("DB_PATH", "/legacy")

	for i := 0; i < 3; i++ {
		if got := Get("LIBRARY_DIR", ""); got != "/legacy" {
			t.Fatalf("Get() = %q, want %q", got, "/legacy")
		}
	}
	if _, err := Required("LIBRARY_DIR"); err != nil {
		t.Fatalf("Required() unexpected error: %v", err)
	}

	if count := strings.Count(logs.String(), "DEPRECATED"); count != 1 {
		t.Fatalf("deprecation warnings = %d, want 1; log: %s", count, logs.String())
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name  string
		set   bool
		value string
		want  string
	}{
		{name: "unset", set: false, want: ""},
		{name: "set", set: true, value: "http://proxy:3128", want: "http://proxy:3128"},
		{name: "empty", set: true, value: "", want: ""},
		{name: "blank", set: true, value: "  ", want: ""},
		{name: "trimmed", set: true, value: " 5 ", want: "5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			if tc.set {
				t.Setenv("SCAN_PARALLELISM", tc.value)
			}

			if got := Lookup("SCAN_PARALLELISM"); got != tc.want {
				t.Fatalf("Lookup() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLegacyEnvMatrix documents the rename matrix: it must stay in sync with
// README.md and .env.example, and it is what T-16 will remove.
func TestLegacyEnvMatrix(t *testing.T) {
	want := map[string]string{
		"LIBRARY_DIR": "DB_PATH",
		"DB_DSN":      "DB_CONNECTION_STRING",
	}
	if len(LegacyEnv) != len(want) {
		t.Fatalf("LegacyEnv = %v, want %v", LegacyEnv, want)
	}
	for primary, legacy := range want {
		if LegacyEnv[primary] != legacy {
			t.Fatalf("LegacyEnv[%q] = %q, want %q", primary, LegacyEnv[primary], legacy)
		}
	}
}
