// Package claudecode adapts the gate to Claude Code's PreToolUse hook
// (docs/design/behavior.md, "The gate (Claude Code)").
//
// The event arrives as JSON on stdin. An allow writes nothing and exits 0. A
// deny writes the hook's decision JSON to stdout, the same reason as the
// first line of stderr, and exits 2. Claude Code proceeds on every other
// exit code, so nothing here returns one, and a panic is a deny.
//
// Nothing from the event is ever written back: a command or a path can carry
// a value (principle 2). A reason is a class from the vocabulary and the
// gate's own message.
package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// Agent is the value of `pbipx gate --agent` this adapter serves.
const Agent = "claude-code"

// Exit codes. Claude Code reads 0 as "proceed" and 2 as "block"; any other
// code is a non-blocking error and the call proceeds.
const (
	ExitAllow = 0
	ExitDeny  = 2
)

// Reason classes the adapter itself produces (testdata/corpus/vocabulary.toml,
// gate-reasons).
const (
	ReasonEventError    = "event-error"
	ReasonInternalError = "internal-error"
)

// Tools, as the gate names them.
const (
	toolRead   gate.Tool = "read"
	toolSearch gate.Tool = "search"
	toolShell  gate.Tool = "shell"
	toolWrite  gate.Tool = "write"
	toolPS     gate.Tool = "powershell"
	toolLSP    gate.Tool = "lsp"
	toolOther  gate.Tool = "other"
)

// maxEvent bounds the event read from stdin. A Write or Edit event carries
// the file's content, so it is generous; a larger event denies.
const maxEvent = 64 << 20

// maxField bounds the command line or path one event carries. The gate's
// work grows with the command's length, and a hook that runs past Claude
// Code's timeout lets the call through, so a longer one denies.
const maxField = 1 << 20

// maxPaths bounds the paths an LSP event names, which are judged one by
// one: more deny, as a long field does.
const maxPaths = 64

// errEvent is the only parse error. Its text is fixed: the event's own text
// is never quoted.
var errEvent = errors.New("the hook event is not a valid PreToolUse JSON object")

// Deny is a deny decision of the given class.
func Deny(reason, message string) gate.Decision {
	return gate.Decision{Allow: false, Reason: reason, Message: message}
}

// ParseEvent reads a PreToolUse event.
//
//   - Read is a read of tool_input.file_path.
//   - Grep is a search of tool_input.path (empty: the working directory).
//   - Bash is a shell command, tool_input.command.
//   - PowerShell is a PowerShell command, tool_input.command, which must be
//     present: the gate cannot parse it and judges it as touching every
//     protected root it can reach.
//   - LSP is a code intelligence call. Its input is not documented, so
//     every string field whose name holds "path", "file" or "uri" (case
//     ignored) is taken as a path, a file:// URI decoded; such a field that
//     is not a string, or an input that is not an object, is errEvent. With
//     no path the gate takes the call to touch the working directory's
//     whole project.
//   - Write, Edit and MultiEdit are a write of tool_input.file_path, and
//     NotebookEdit a write of tool_input.notebook_path, or file_path when
//     there is none; a NotebookEdit with neither is errEvent.
//   - Every other tool is "other", Glob included: it prints file names, and
//     names are schema (principle 1). behavior.md lists Read, Grep, Bash,
//     PowerShell, LSP and the writing tools as the gated tools; every tool
//     not listed is allowed.
//
// The event must be a JSON object; tool_name and cwd, when present, strings;
// tool_input, when present, an object; and the field the tool is judged by,
// when present, a string. Anything else is errEvent.
func ParseEvent(data []byte) (gate.Event, error) {
	var raw struct {
		ToolName  *string                    `json:"tool_name"`
		Cwd       *string                    `json:"cwd"`
		ToolInput map[string]json.RawMessage `json:"tool_input"`
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return gate.Event{}, errEvent
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return gate.Event{}, errEvent
	}
	var ev gate.Event
	if raw.Cwd != nil {
		ev.Cwd = *raw.Cwd
	}
	name := ""
	if raw.ToolName != nil {
		name = *raw.ToolName
	}
	field := func(key string) (string, error) {
		v, ok := raw.ToolInput[key]
		if !ok || string(v) == "null" {
			return "", nil
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", errEvent
		}
		return s, nil
	}
	var err error
	switch name {
	case "Read":
		ev.Tool = toolRead
		ev.Path, err = field("file_path")
	case "Grep":
		ev.Tool = toolSearch
		ev.Path, err = field("path")
	case "Bash":
		ev.Tool = toolShell
		ev.Command, err = field("command")
	case "Write", "Edit", "MultiEdit":
		ev.Tool = toolWrite
		ev.Path, err = field("file_path")
	case "PowerShell":
		ev.Tool = toolPS
		ev.Command, err = field("command")
		if err == nil && ev.Command == "" {
			err = errEvent
		}
	case "LSP":
		ev.Tool = toolLSP
		ev.Paths, err = lspPaths(raw.ToolInput)
	case "NotebookEdit":
		ev.Tool = toolWrite
		ev.Path, err = field("notebook_path")
		if err == nil && ev.Path == "" {
			ev.Path, err = field("file_path")
		}
		if err == nil && ev.Path == "" {
			err = errEvent
		}
	default:
		ev.Tool = toolOther
	}
	if err != nil {
		return gate.Event{}, err
	}
	return ev, nil
}

// Run is the whole hook: it reads the event from stdin, decides it with
// gate.Decide and renders the decision. It returns ExitAllow or ExitDeny,
// and never anything else; a panic anywhere below is a deny.
func Run(stdin io.Reader, stdout, stderr io.Writer, opts gate.Options) (code int) {
	return runWithin(stdin, stdout, stderr, opts, Watchdog, decide)
}

// Watchdog is how long the gate decides before it denies, whatever the
// decision code is doing: a hook that times out lets the call proceed, so
// a hang becomes a deny well before the hook timeout pbipx init sets (300
// seconds).
const Watchdog = 120 * time.Second

// runWithin is Run with the decision made by decideFn, which is denied
// with internal-error when it panics or takes longer than limit. Past the
// limit the decision goes on, discarded, until the process exits.
func runWithin(stdin io.Reader, stdout, stderr io.Writer, opts gate.Options, limit time.Duration,
	decideFn func(io.Reader, gate.Options) gate.Decision,
) (code int) {
	defer func() {
		if recover() != nil {
			code = Render(Deny(ReasonInternalError, "the gate failed"), stdout, stderr)
		}
	}()
	done := make(chan gate.Decision, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- Deny(ReasonInternalError, "the gate failed")
			}
		}()
		done <- decideFn(stdin, opts)
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case d := <-done:
		return Render(d, stdout, stderr)
	case <-timer.C:
		return Render(Deny(ReasonInternalError, "the gate took too long to decide"), stdout, stderr)
	}
}

func decide(stdin io.Reader, opts gate.Options) gate.Decision {
	data, err := io.ReadAll(io.LimitReader(stdin, maxEvent+1))
	if err != nil {
		return Deny(ReasonEventError, "the hook event could not be read")
	}
	if len(data) > maxEvent {
		return Deny(ReasonEventError, "the hook event is too large")
	}
	ev, err := ParseEvent(data)
	if err != nil {
		return Deny(ReasonEventError, err.Error())
	}
	paths := 0
	for _, p := range ev.Paths {
		paths += len(p)
	}
	if len(ev.Command) > maxField || len(ev.Path) > maxField || paths > maxField || len(ev.Paths) > maxPaths {
		return Deny(ReasonInternalError, "the command or path is too long to judge")
	}
	return gate.Decide(ev, opts)
}

// classPattern is a reason class: what the corpus runner reads from
// "pbipx: <class>:".
var classPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Reason is a deny's text: "pbipx: <class>: <message>", on one line. A
// decision without a valid class is an internal error.
func Reason(d gate.Decision) string {
	class, msg := d.Reason, d.Message
	if !classPattern.MatchString(class) {
		class, msg = ReasonInternalError, "the gate returned a deny without a reason"
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if msg == "" {
		return "pbipx: " + class
	}
	return "pbipx: " + class + ": " + msg
}

type hookOutput struct {
	HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
}

type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// Render writes a decision and returns the exit code. An allow writes
// nothing. A deny writes the decision JSON to stdout and the reason as the
// first line of stderr; write errors do not change the exit code, which is
// the decision.
func Render(d gate.Decision, stdout, stderr io.Writer) int {
	if d.Allow {
		return ExitAllow
	}
	reason := Reason(d)
	out, err := json.Marshal(hookOutput{HookSpecificOutput: hookSpecific{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}})
	if err == nil {
		_, _ = fmt.Fprintf(stdout, "%s\n", out)
	}
	_, _ = fmt.Fprintln(stderr, reason)
	return ExitDeny
}
