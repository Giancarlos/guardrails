package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}

// withTempDB sets up a fresh sqlite db in a temp dir and tears it down. Returns the project root.
func withTempDB(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	// Resolve symlinks so the path matches os.Getwd() (macOS /var -> /private/var).
	if resolved, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = resolved
	}
	guardrailsDir := filepath.Join(tmpDir, ".guardrails")
	if err := os.MkdirAll(guardrailsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(guardrailsDir, "db.sqlite")
	t.Setenv("GUR_DB_PATH", dbPath)
	if _, err := db.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.CloseDB() })

	// Run inside the temp project so FindProjectRoot resolves.
	origCwd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origCwd) })
	return tmpDir
}

// withVerboseOutput forces non-compact, non-JSON output for the duration of the test.
func withVerboseOutput(t *testing.T) {
	t.Helper()
	prevVerbose, prevJSON := verboseOutput, jsonOutput
	verboseOutput = true
	jsonOutput = false
	t.Cleanup(func() {
		verboseOutput = prevVerbose
		jsonOutput = prevJSON
	})
}

func TestWhoamiIncludesMode(t *testing.T) {
	withTempDB(t)
	withVerboseOutput(t)

	if err := db.SetConfig(models.ConfigMode, models.ModeStealth); err != nil {
		t.Fatalf("set mode: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhoami(nil, nil); err != nil {
			t.Fatalf("runWhoami: %v", err)
		}
	})

	if !strings.Contains(out, "Mode:") {
		t.Errorf("whoami output missing 'Mode:' line:\n%s", out)
	}
	if !strings.Contains(out, models.ModeStealth) {
		t.Errorf("whoami output missing mode value %q:\n%s", models.ModeStealth, out)
	}
}

func TestWhoamiModeDefaultsWhenUnset(t *testing.T) {
	withTempDB(t)
	withVerboseOutput(t)

	out := captureStdout(t, func() {
		if err := runWhoami(nil, nil); err != nil {
			t.Fatalf("runWhoami: %v", err)
		}
	})

	if !strings.Contains(out, "Mode:     "+models.ModeDefault) {
		t.Errorf("expected mode to default to %q:\n%s", models.ModeDefault, out)
	}
}

func TestWhoamiJSONIncludesMode(t *testing.T) {
	withTempDB(t)
	prevVerbose, prevJSON := verboseOutput, jsonOutput
	verboseOutput = false
	jsonOutput = true
	t.Cleanup(func() { verboseOutput = prevVerbose; jsonOutput = prevJSON })

	if err := db.SetConfig(models.ConfigMode, models.ModeContributor); err != nil {
		t.Fatalf("set mode: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runWhoami(nil, nil); err != nil {
			t.Fatalf("runWhoami: %v", err)
		}
	})

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got := parsed["mode"]; got != models.ModeContributor {
		t.Errorf("json mode = %v, want %q", got, models.ModeContributor)
	}
}

func TestHashHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantLen  int
	}{
		{"simple hostname", "localhost", 8},
		{"fqdn", "server.example.com", 8},
		{"empty", "", 8},
		{"with special chars", "my-server_01", 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := sha256.Sum256([]byte(tt.hostname))
			hash := hex.EncodeToString(h[:])[:8]

			if len(hash) != tt.wantLen {
				t.Errorf("hashHostname() len = %d, want %d", len(hash), tt.wantLen)
			}

			// Verify deterministic
			h2 := sha256.Sum256([]byte(tt.hostname))
			hash2 := hex.EncodeToString(h2[:])[:8]
			if hash != hash2 {
				t.Errorf("hashHostname() not deterministic: %s != %s", hash, hash2)
			}
		})
	}
}

func TestHashHostnameUniqueness(t *testing.T) {
	hostnames := []string{"localhost", "server1", "server2", "my-machine.local"}
	hashes := make(map[string]string)

	for _, hostname := range hostnames {
		h := sha256.Sum256([]byte(hostname))
		hash := hex.EncodeToString(h[:])[:8]

		if existing, ok := hashes[hash]; ok {
			t.Errorf("hash collision: %s and %s both hash to %s", existing, hostname, hash)
		}
		hashes[hash] = hostname
	}
}

func TestGetHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		// Some environments may not have a hostname
		t.Skip("hostname not available")
	}

	if hostname == "" {
		t.Error("hostname is empty")
	}
}
