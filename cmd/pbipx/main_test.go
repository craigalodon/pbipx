package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
)

// sentinel stands for a value typed on a command line. It must never be
// echoed.
const sentinel = "Doe, Jane"

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no command", nil, finding.ExitUsage, "", "Usage:"},
		{"version", []string{"version"}, 0, "pbipx dev\n", ""},
		{"version flag", []string{"--version"}, 0, "pbipx dev\n", ""},
		{"help", []string{"help"}, 0, "Usage:", ""},
		{"help flag", []string{"--help"}, 0, "Usage:", ""},
		{"help for a command", []string{"help", "version"}, 0, "print the version", ""},
		{"unknown command", []string{sentinel}, finding.ExitUsage, "", "usage error"},
		{"unknown help topic", []string{"help", sentinel}, finding.ExitUsage, "", "usage error"},
		{"unknown flag", []string{"--" + sentinel}, finding.ExitUsage, "", "usage error"},
		{"flag with a value", []string{"--name=" + sentinel}, finding.ExitUsage, "", "usage error"},
		{"extra argument", []string{"version", sentinel}, finding.ExitUsage, "", "usage error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, strings.NewReader(""), &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			for _, part := range []string{"Doe", "Jane"} {
				if strings.Contains(stdout.String()+stderr.String(), part) {
					t.Errorf("output echoes the argument")
				}
			}
		})
	}
}
