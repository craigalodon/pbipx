package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestGateExitsOnlyZeroOrTwo checks that every failure of `pbipx gate`
// itself, including a usage error and a help request, is a deny with exit 2:
// Claude Code proceeds on any other code.
func TestGateExitsOnlyZeroOrTwo(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		class string
	}{
		{"unknown flag", []string{"gate", "--" + sentinel}, "{}", "internal-error"},
		{"flag with a value", []string{"gate", "--name=" + sentinel}, "{}", "internal-error"},
		{"other agent", []string{"gate", "--agent", sentinel}, "{}", "internal-error"},
		{"empty agent", []string{"gate", "--agent="}, "{}", "internal-error"},
		{"agent without a value", []string{"gate", "--agent"}, "{}", "internal-error"},
		{"extra argument", []string{"gate", sentinel}, "{}", "internal-error"},
		{"help flag", []string{"gate", "--help"}, "{}", "internal-error"},
		{"short help flag", []string{"gate", "-h"}, "{}", "internal-error"},
		{"version flag", []string{"gate", "--version"}, "{}", "internal-error"},
		{"not json", []string{"gate"}, sentinel, "event-error"},
		{"array", []string{"gate", "--agent", "claude-code"}, `["` + sentinel + `"]`, "event-error"},
		{"no event", []string{"gate", "--agent=claude-code"}, "", "event-error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			first, _, _ := strings.Cut(stderr.String(), "\n")
			if !strings.HasPrefix(first, "pbipx: "+tt.class+":") {
				t.Errorf("stderr does not start with \"pbipx: %s:\"", tt.class)
			}
			var out struct {
				HookSpecificOutput struct {
					PermissionDecision       string `json:"permissionDecision"`
					PermissionDecisionReason string `json:"permissionDecisionReason"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatalf("stdout is not the hook JSON: %v", err)
			}
			if out.HookSpecificOutput.PermissionDecision != "deny" || out.HookSpecificOutput.PermissionDecisionReason != first {
				t.Errorf("hook JSON is not a deny with stderr's reason")
			}
			for _, part := range []string{"Doe", "Jane"} {
				if strings.Contains(stdout.String()+stderr.String(), part) {
					t.Errorf("output echoes the input")
				}
			}
		})
	}
}

// TestGateHelp checks that help for the gate is still available as a
// command, where it decides nothing.
func TestGateHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "gate"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "pre-tool hook") {
		t.Error("help for the gate does not describe it")
	}
}
