package claudecode

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// sentinel stands for a value in an event. It must never be echoed.
const sentinel = "Roe, Richard"

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name  string
		event string
		want  gate.Event
	}{
		{"read", `{"hook_event_name":"PreToolUse","cwd":"/r","tool_name":"Read","tool_input":{"file_path":"/r/a.json"}}`,
			gate.Event{Tool: toolRead, Path: "/r/a.json", Cwd: "/r"}},
		{"grep with a path", `{"cwd":"/r","tool_name":"Grep","tool_input":{"pattern":"x","path":"/r/pbip"}}`,
			gate.Event{Tool: toolSearch, Path: "/r/pbip", Cwd: "/r"}},
		{"grep without a path", `{"cwd":"/r","tool_name":"Grep","tool_input":{"pattern":"x"}}`,
			gate.Event{Tool: toolSearch, Cwd: "/r"}},
		// A glob narrows nothing the gate judges: the search covers path.
		{"grep with a glob", `{"cwd":"/r","tool_name":"Grep","tool_input":{"pattern":"x","glob":"*.md","output_mode":"content","-i":true}}`,
			gate.Event{Tool: toolSearch, Cwd: "/r"}},
		{"glob is other", `{"cwd":"/r","tool_name":"Glob","tool_input":{"pattern":"**/*.json","path":"/r/pbip"}}`,
			gate.Event{Tool: toolOther, Cwd: "/r"}},
		{"bash", `{"cwd":"/r","tool_name":"Bash","tool_input":{"command":"ls -la"}}`,
			gate.Event{Tool: toolShell, Command: "ls -la", Cwd: "/r"}},
		{"write", `{"cwd":"/r","tool_name":"Write","tool_input":{"file_path":"/r/n.md","content":"x"}}`,
			gate.Event{Tool: toolWrite, Path: "/r/n.md", Cwd: "/r"}},
		{"edit", `{"cwd":"/r","tool_name":"Edit","tool_input":{"file_path":"n.md","old_string":"a","new_string":"b"}}`,
			gate.Event{Tool: toolEdit, Path: "n.md", Cwd: "/r"}},
		{"multiedit", `{"cwd":"/r","tool_name":"MultiEdit","tool_input":{"file_path":"/r/n.md","edits":[]}}`,
			gate.Event{Tool: toolEdit, Path: "/r/n.md", Cwd: "/r"}},
		{"notebookedit", `{"cwd":"/r","tool_name":"NotebookEdit","tool_input":{"notebook_path":"/r/a.ipynb","new_source":"x"}}`,
			gate.Event{Tool: toolEdit, Path: "/r/a.ipynb", Cwd: "/r"}},
		{"notebookedit falls back to file_path", `{"cwd":"/r","tool_name":"NotebookEdit","tool_input":{"file_path":"/r/b"}}`,
			gate.Event{Tool: toolEdit, Path: "/r/b", Cwd: "/r"}},
		{"notebookedit prefers notebook_path", `{"cwd":"/r","tool_name":"NotebookEdit","tool_input":{"notebook_path":"/r/a.ipynb","file_path":"/r/b"}}`,
			gate.Event{Tool: toolEdit, Path: "/r/a.ipynb", Cwd: "/r"}},
		{"powershell", `{"cwd":"/r","tool_name":"PowerShell","tool_input":{"command":"Get-ChildItem -Recurse","description":"x"}}`,
			gate.Event{Tool: toolPS, Command: "Get-ChildItem -Recurse", Cwd: "/r"}},
		{"lsp with a file path", `{"cwd":"/r","tool_name":"LSP","tool_input":{"operation":"hover","filePath":"/r/pbip/a.json","line":3}}`,
			gate.Event{Tool: toolLSP, Paths: []string{"/r/pbip/a.json"}, Cwd: "/r"}},
		{"lsp with a file uri", `{"cwd":"/r","tool_name":"LSP","tool_input":{"textDocument":{"uri":"file:///r/a%20b.go"}}}`,
			gate.Event{Tool: toolLSP, Paths: []string{"/r/a b.go"}, Cwd: "/r"}},
		{"lsp with a windows file uri", `{"cwd":"C:\\r","tool_name":"LSP","tool_input":{"uri":"file:///C:/r/x.go"}}`,
			gate.Event{Tool: toolLSP, Paths: []string{"C:/r/x.go"}, Cwd: `C:\r`}},
		{"lsp with several paths", `{"cwd":"/r","tool_name":"LSP","tool_input":{"path":"a","files":["b","c"],"name":"d"}}`,
			gate.Event{Tool: toolLSP, Paths: []string{"b", "c", "a"}, Cwd: "/r"}},
		{"lsp with no path", `{"cwd":"/r","tool_name":"LSP","tool_input":{"operation":"workspaceSymbol","query":"x"}}`,
			gate.Event{Tool: toolLSP, Cwd: "/r"}},
		{"no tool name", `{"cwd":"/r"}`, gate.Event{Tool: toolOther, Cwd: "/r"}},
		{"read without input", `{"cwd":"/r","tool_name":"Read"}`, gate.Event{Tool: toolRead, Cwd: "/r"}},
		{"null field", `{"tool_name":"Read","tool_input":{"file_path":null}}`, gate.Event{Tool: toolRead}},
		{"leading space", " \n{\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"pwd\"}}\n",
			gate.Event{Tool: toolShell, Command: "pwd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEvent([]byte(tt.event))
			if err != nil {
				t.Fatalf("ParseEvent: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseEvent = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseEventRejects(t *testing.T) {
	for name, event := range map[string]string{
		"empty":                    "",
		"not json":                 "Roe, Richard",
		"array":                    `[{"tool_name":"Read"}]`,
		"null":                     "null",
		"string":                   `"Roe, Richard"`,
		"number":                   "2",
		"truncated":                `{"tool_name":"Read","tool_input":{"file_path":"Roe, Richard`,
		"trailing data":            `{"tool_name":"Read"} {"tool_name":"Bash"}`,
		"tool name a number":       `{"tool_name":2}`,
		"cwd an object":            `{"cwd":{"a":"Roe, Richard"},"tool_name":"Read"}`,
		"input an array":           `{"tool_name":"Read","tool_input":["Roe, Richard"]}`,
		"path a number":            `{"tool_name":"Read","tool_input":{"file_path":2}}`,
		"command an array":         `{"tool_name":"Bash","tool_input":{"command":["Roe, Richard"]}}`,
		"write path a number":      `{"tool_name":"Write","tool_input":{"file_path":2,"content":"Roe, Richard"}}`,
		"notebook path an object":  `{"tool_name":"NotebookEdit","tool_input":{"notebook_path":{"a":"Roe, Richard"}}}`,
		"notebook without a path":  `{"tool_name":"NotebookEdit","tool_input":{"new_source":"Roe, Richard"}}`,
		"notebook path empty":      `{"tool_name":"NotebookEdit","tool_input":{"notebook_path":""}}`,
		"powershell no command":    `{"tool_name":"PowerShell","tool_input":{"description":"Roe, Richard"}}`,
		"powershell no input":      `{"tool_name":"PowerShell"}`,
		"powershell command array": `{"tool_name":"PowerShell","tool_input":{"command":["Roe, Richard"]}}`,
		"lsp path a number":        `{"tool_name":"LSP","tool_input":{"filePath":2}}`,
		"lsp uri a bool":           `{"tool_name":"LSP","tool_input":{"textDocument":{"uri":true}}}`,
		"lsp bad file uri":         `{"tool_name":"LSP","tool_input":{"uri":"file://%zz/Roe, Richard"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseEvent([]byte(event))
			if err == nil {
				t.Fatal("ParseEvent accepted the event")
			}
			if strings.Contains(err.Error(), "Roe") {
				t.Error("the error echoes the event")
			}
		})
	}
}

// hookJSON is the deny Claude Code reads from stdout.
type hookJSON struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func TestRender(t *testing.T) {
	t.Run("allow", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Render(gate.Decision{Allow: true, Reason: "finding", Message: "ignored"}, &stdout, &stderr); code != ExitAllow {
			t.Errorf("exit = %d, want %d", code, ExitAllow)
		}
		if stdout.Len() != 0 || stderr.Len() != 0 {
			t.Error("an allow wrote output")
		}
	})
	tests := []struct {
		name string
		d    gate.Decision
		want string
	}{
		{"deny", Deny("finding", "pbip/a.json line 3: full_name (block column)"),
			"pbipx: finding: pbip/a.json line 3: full_name (block column)"},
		{"multi-line message", Deny("scan-error", "first\nsecond\r\nthird"), "pbipx: scan-error: first second third"},
		{"no message", Deny("policy-error", ""), "pbipx: policy-error"},
		{"no class", Deny("", "x"), "pbipx: internal-error: the gate returned a deny without a reason"},
		{"bad class", Deny("Finding: x", "x"), "pbipx: internal-error: the gate returned a deny without a reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Render(tt.d, &stdout, &stderr); code != ExitDeny {
				t.Errorf("exit = %d, want %d", code, ExitDeny)
			}
			first, _, _ := strings.Cut(stderr.String(), "\n")
			if first != tt.want {
				t.Errorf("stderr line 1 = %q, want %q", first, tt.want)
			}
			var h hookJSON
			if err := json.Unmarshal(stdout.Bytes(), &h); err != nil {
				t.Fatalf("stdout is not JSON: %v", err)
			}
			o := h.HookSpecificOutput
			if o.HookEventName != "PreToolUse" || o.PermissionDecision != "deny" || o.PermissionDecisionReason != tt.want {
				t.Errorf("hook JSON = %+v", o)
			}
		})
	}
}

// panicReader panics on read, standing for a failure anywhere below Run.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic(sentinel) }

func TestRunDeniesWithoutDeciding(t *testing.T) {
	tests := []struct {
		name  string
		stdin io.Reader
		class string
	}{
		{"not json", strings.NewReader(sentinel), ReasonEventError},
		{"array", strings.NewReader(`["` + sentinel + `"]`), ReasonEventError},
		{"empty", strings.NewReader(""), ReasonEventError},
		{"too large", bytes.NewReader(make([]byte, maxEvent+1)), ReasonEventError},
		{"panic", panicReader{}, ReasonInternalError},
		{"command too long", strings.NewReader(`{"tool_name":"Bash","cwd":"/","tool_input":{"command":"` +
			strings.Repeat(sentinel+" ", maxField/len(sentinel)+1) + `"}}`), ReasonInternalError},
		{"path too long", strings.NewReader(`{"tool_name":"Read","cwd":"/","tool_input":{"file_path":"` +
			strings.Repeat("a/", maxField/2+1) + `"}}`), ReasonInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.stdin, &stdout, &stderr, gate.Options{})
			if code != ExitDeny {
				t.Errorf("exit = %d, want %d", code, ExitDeny)
			}
			if !strings.HasPrefix(stderr.String(), "pbipx: "+tt.class+":") {
				t.Errorf("stderr does not start with the class %s", tt.class)
			}
			if strings.Contains(stdout.String()+stderr.String(), "Roe") {
				t.Error("the output echoes the event")
			}
		})
	}
}
