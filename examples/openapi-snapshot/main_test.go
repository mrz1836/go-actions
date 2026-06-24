package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWriteThenCheck(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), snapshotPath)

	var out bytes.Buffer
	if err := run("write", path, &out); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	out.Reset()
	if err := run("check", path, &out); err != nil {
		t.Fatalf("check after write: %v", err)
	}
}

func TestRunCheckDetectsDrift(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), snapshotPath)
	if err := os.WriteFile(path, []byte(`{"openapi":"3.1.0"}`), 0o600); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	if err := run("check", path, io.Discard); !errors.Is(err, errDrift) {
		t.Fatalf("err = %v, want errDrift", err)
	}
}

func TestRunCheckMissingSnapshot(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "absent.json")

	if err := run("check", path, io.Discard); err == nil {
		t.Fatal("expected an error reading an absent snapshot")
	}
}

func TestRunUnknownMode(t *testing.T) {
	t.Parallel()
	if err := run("frobnicate", "ignored.json", io.Discard); !errors.Is(err, errUnknownMode) {
		t.Fatalf("err = %v, want errUnknownMode", err)
	}
}
