package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no command", nil, 2, "", "Usage:"},
		{"version", []string{"version"}, 0, "pbipx dev\n", ""},
		{"help", []string{"help"}, 0, "Usage:", ""},
		{"unknown", []string{"Doe, Jane"}, 2, "", "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			for _, arg := range tt.args {
				if tt.wantCode != 0 && strings.Contains(stdout.String()+stderr.String(), arg) {
					t.Errorf("output echoes the argument")
				}
			}
		})
	}
}
