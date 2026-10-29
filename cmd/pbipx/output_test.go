package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A character device that is not a terminal, such as the null device, must
// not admit --show-values.
func TestNullDeviceIsNotATerminal(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip("no null device")
	}
	defer func() { _ = null.Close() }()
	if info, err := null.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("the null device is not a character device here")
	}
	if isTerminal(null) {
		t.Error("the null device counts as a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	if isTerminal(w) || isTerminal(&bytes.Buffer{}) {
		t.Error("a pipe or a buffer counts as a terminal")
	}
}

func TestShowValuesRefusedOnNullDevice(t *testing.T) {
	newRepo(t, "repo")
	stdoutIsTerminal = isTerminal
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip("no null device")
	}
	defer func() { _ = null.Close() }()
	var stderr bytes.Buffer
	code := run([]string{"scan-text", "--show-values", "notes/review.md"}, strings.NewReader(""), null, &stderr)
	if code != 4 || !strings.Contains(stderr.String(), "not a terminal") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}
