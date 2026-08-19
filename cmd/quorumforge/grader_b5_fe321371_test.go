package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldB5_fe321371_CLIRejectsUnsupportedPositionalArguments(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "quorumforge")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build quorumforge: %v\n%s", err, output)
	}

	command := exec.Command(binaryPath, "-db", ":memory:", "-seed=false", "-addr", "bad", "unexpected")
	output, err := command.CombinedOutput()
	message := string(output)

	t.Run("reports the unsupported trailing argument", func(t *testing.T) {
		if err == nil {
			t.Fatal("command succeeded with an unsupported positional argument")
		}
		if !strings.Contains(message, "unexpected") || !strings.Contains(message, "unsupported positional argument") {
			t.Fatalf("error does not identify the unsupported positional argument:\n%s", message)
		}
	})

	t.Run("stops before the HTTP service boundary", func(t *testing.T) {
		if strings.Contains(message, "quorumforge listening") {
			t.Fatalf("command reached HTTP startup:\n%s", message)
		}
		if strings.Contains(message, "missing port in address") {
			t.Fatalf("later listen error masked the invalid argument:\n%s", message)
		}
	})

	t.Run("repeated rejection has no persistence effect", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "must-not-exist.db")
		for attempt := 0; attempt < 2; attempt++ {
			command := exec.Command(binaryPath, "-db", databasePath, "-seed=false", "-addr", "bad", "unexpected")
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("attempt %d succeeded with an unsupported positional argument", attempt+1)
			}
			if !strings.Contains(string(output), "unexpected") {
				t.Fatalf("attempt %d did not report the invalid argument:\n%s", attempt+1, output)
			}
			if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
				t.Fatalf("attempt %d opened the database before rejecting arguments: stat error = %v", attempt+1, statErr)
			}
		}
	})

	t.Run("retains startup for option-only input", func(t *testing.T) {
		command := exec.Command(binaryPath, "-db", ":memory:", "-seed=false", "-addr", "bad")
		output, err := command.CombinedOutput()
		message := string(output)
		if err == nil {
			t.Fatal("command unexpectedly accepted the deliberately invalid listen address")
		}
		if !strings.Contains(message, "quorumforge listening on bad") || !strings.Contains(message, "missing port in address") {
			t.Fatalf("option-only input no longer reaches the existing listen-address path:\n%s", message)
		}
	})
}
