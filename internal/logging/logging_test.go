package logging

import (
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// captureStdout runs body with os.Stdout replaced by a temporary file and
// returns everything written to it.
func captureStdout(t *testing.T, body func()) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = originalStdout }()

	body()

	if err := file.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(data)
}

// restoreLogging brings the global logging state back to what it was before the
// test, so that Setup in one test does not leak into another.
func restoreLogging(t *testing.T) {
	t.Helper()
	logger := slog.Default()
	writer := log.Writer()
	flags := log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(logger)
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
}

func TestSetupEmptyEnvironment(t *testing.T) {
	restoreLogging(t)
	t.Setenv(levelKey, "")
	t.Setenv(formatKey, "")

	out := captureStdout(t, func() {
		Setup()
		slog.Info("hello from slog")
	})

	if out == "" {
		t.Fatal("Setup() produced no output")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &record); err != nil {
		t.Fatalf("default format is not JSON: %v (output %q)", err, out)
	}
	if record["msg"] != "hello from slog" {
		t.Fatalf("unexpected record: %v", record)
	}
	if record["level"] != "INFO" {
		t.Fatalf("unexpected level: %v", record["level"])
	}
}

func TestSetupJSONFormatIsValidJSONPerLine(t *testing.T) {
	restoreLogging(t)
	t.Setenv(levelKey, "info")
	t.Setenv(formatKey, "json")

	out := captureStdout(t, func() {
		Setup()
		slog.Warn("first", "n", 1)
		slog.Warn("second", "n", 2)
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), out)
	}
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not valid JSON: %v (%q)", i, err, line)
		}
	}
}

func TestSetupTextFormat(t *testing.T) {
	restoreLogging(t)
	t.Setenv(levelKey, "info")
	t.Setenv(formatKey, "text")

	out := captureStdout(t, func() {
		Setup()
		slog.Info("readable message", "key", "value")
	})

	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("text format produced JSON: %q", out)
	}
	for _, want := range []string{"readable message", "key=value"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestSetupRedirectsStandardLog(t *testing.T) {
	restoreLogging(t)
	t.Setenv(levelKey, "info")
	t.Setenv(formatKey, "json")

	out := captureStdout(t, func() {
		Setup()
		log.Printf("processing archive %s", "book.zip")
	})

	if !strings.Contains(out, `"msg":"processing archive book.zip"`) {
		t.Fatalf("standard log record is not structured: %q", out)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &record); err != nil {
		t.Fatalf("standard log record is not JSON: %v", err)
	}
	if _, ok := record["time"]; !ok {
		t.Fatalf("record has no timestamp: %v", record)
	}
}

func TestSetupUnknownValuesDoNotPanic(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		format      string
		wantMessage string
	}{
		{name: "unknown level", level: "verbose", format: "json", wantMessage: "unknown log level"},
		{name: "unknown format", level: "info", format: "yaml", wantMessage: "unknown log format"},
		{name: "both unknown", level: "loud", format: "xml", wantMessage: "unknown log level"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreLogging(t)
			t.Setenv(levelKey, tc.level)
			t.Setenv(formatKey, tc.format)

			out := captureStdout(t, func() {
				Setup()
				slog.Info("still running")
			})

			if !strings.Contains(out, tc.wantMessage) {
				t.Fatalf("output %q does not warn about %q", out, tc.wantMessage)
			}
			if !strings.Contains(out, "still running") {
				t.Fatalf("logging stopped after a bad value: %q", out)
			}
		})
	}
}

func TestSetupHonoursLevel(t *testing.T) {
	tests := []struct {
		level          string
		wantDebug      bool
		wantInfo       bool
		wantStdlibInfo bool
	}{
		{level: "debug", wantDebug: true, wantInfo: true, wantStdlibInfo: true},
		{level: "info", wantDebug: false, wantInfo: true, wantStdlibInfo: true},
		{level: "warn", wantDebug: false, wantInfo: false, wantStdlibInfo: false},
		{level: "error", wantDebug: false, wantInfo: false, wantStdlibInfo: false},
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			restoreLogging(t)
			t.Setenv(levelKey, tc.level)
			t.Setenv(formatKey, "json")

			out := captureStdout(t, func() {
				Setup()
				slog.Debug("debug message", "target", "all")
				slog.Info("info message", "target", "all")
				log.Printf("stdlib message")
			})

			assertVisible(t, out, "debug message", tc.wantDebug, tc.level)
			assertVisible(t, out, "info message", tc.wantInfo, tc.level)
			assertVisible(t, out, "stdlib message", tc.wantStdlibInfo, tc.level)
		})
	}
}

func assertVisible(t *testing.T, output, msg string, want bool, level string) {
	t.Helper()
	visible := strings.Contains(output, `"msg":"`+msg+`"`)
	if visible != want {
		t.Fatalf("level %s: %q visible = %v, want %v (output %q)", level, msg, visible, want, output)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "", want: "INFO"},
		{value: "DEBUG", want: "DEBUG"},
		{value: " Info ", want: "INFO"},
		{value: "WARN", want: "WARN"},
		{value: "Error", want: "ERROR"},
		{value: "nonsense", want: "INFO"},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			level, warning := parseLevel(tc.value)
			if level.String() != tc.want {
				t.Fatalf("parseLevel(%q) = %s, want %s", tc.value, level, tc.want)
			}
			if tc.value == "nonsense" && warning == "" {
				t.Fatal("expected a warning for an unknown level")
			}
			if tc.value != "nonsense" && warning != "" {
				t.Fatalf("unexpected warning %q for value %q", warning, tc.value)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		value      string
		want       string
		wantWarned bool
	}{
		{value: "", want: "json"},
		{value: "JSON", want: "json"},
		{value: "text", want: "text"},
		{value: "yaml", want: "json", wantWarned: true},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			format, warning := parseFormat(tc.value)
			if format != tc.want {
				t.Fatalf("parseFormat(%q) = %q, want %q", tc.value, format, tc.want)
			}
			if (warning != "") != tc.wantWarned {
				t.Fatalf("parseFormat(%q) warning = %q, wantWarned = %v", tc.value, warning, tc.wantWarned)
			}
		})
	}
}
