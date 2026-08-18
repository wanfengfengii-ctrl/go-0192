package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestGoldB1_c4b2fc98_helpExitsZero covers the public CLI contract for -h.
// The test lives in the submitted repository so the verification command is
// independently runnable from a clean clone without a grader overlay.
func TestGoldB1_c4b2fc98_helpExitsZero(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-h")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("go run . -h exited with error: %v\n%s", err, output.String())
	}
	text := output.String()
	if !strings.Contains(text, "Usage of quorumforge:") {
		t.Fatalf("help output missing usage header: %s", text)
	}
	if strings.Contains(text, "quorumforge: flag: help requested") {
		t.Fatalf("help request was reported as an error: %s", text)
	}
}
