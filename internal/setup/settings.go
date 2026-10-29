package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HookMatcher is the tools the gate's PreToolUse hook is registered for.
const HookMatcher = "Read|Grep|Bash|Write|Edit|MultiEdit|NotebookEdit"

// gateArgs is in every pbipx gate hook command (claudecode.HookCommand), and
// identifies one that runs another binary.
const gateArgs = " gate --agent claude-code"

// Hook is one command hook for Claude Code.
type Hook struct {
	Command string
	Shell   string // "" to leave the field out
}

type hookJSON struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Shell   string `json:"shell,omitempty"`
}

type entryJSON struct {
	Matcher string     `json:"matcher"`
	Hooks   []hookJSON `json:"hooks"`
}

func (h Hook) entry() entryJSON {
	return entryJSON{Matcher: HookMatcher, Hooks: []hookJSON{{Type: "command", Command: h.Command, Shell: h.Shell}}}
}

// ErrSettings is a settings file pbipx will not change: not a JSON object, or
// with "hooks" or "hooks.PreToolUse" of the wrong type or given twice.
var ErrSettings = errors.New("the settings file is not in the shape pbipx can merge into")

// SettingsPath is the user-level Claude Code settings file:
// $CLAUDE_CONFIG_DIR/settings.json when the variable is set, and
// ~/.claude/settings.json otherwise.
func SettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Abs(filepath.Join(dir, "settings.json"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Merge is the result of merging the hook into a settings file.
type Merge struct {
	// Data is the new file. It is nil when nothing changes.
	Data []byte
	// Present is set when an equivalent hook is already registered: the
	// same matcher, command and shell.
	Present bool
	// Others counts the pbipx gate hooks registered with a different
	// command, such as a binary at another path. They are left in place.
	Others int
}

// MergeHook adds h as a PreToolUse hook to existing, the bytes of a settings
// file (empty for none). Every other key, and every other hook, is kept, in
// its order. The result is indented with two spaces.
func MergeHook(existing []byte, h Hook) (Merge, error) {
	top := []member{}
	if len(bytes.TrimSpace(existing)) > 0 {
		var err error
		if top, err = objectMembers(existing); err != nil {
			return Merge{}, err
		}
	}
	hi, err := find(top, "hooks")
	if err != nil {
		return Merge{}, err
	}
	hooks := []member{}
	if hi >= 0 {
		if hooks, err = objectMembers(top[hi].value); err != nil {
			return Merge{}, err
		}
	}
	pi, err := find(hooks, "PreToolUse")
	if err != nil {
		return Merge{}, err
	}
	var entries []json.RawMessage
	if pi >= 0 {
		if err := json.Unmarshal(hooks[pi].value, &entries); err != nil {
			return Merge{}, ErrSettings
		}
	}

	var m Merge
	want := h.entry().Hooks[0]
	for _, raw := range entries {
		var e entryJSON
		if json.Unmarshal(raw, &e) != nil {
			continue // another tool's entry, in a shape pbipx does not read
		}
		for _, x := range e.Hooks {
			switch {
			case e.Matcher == HookMatcher && x.Type == want.Type && x.Command == want.Command && x.Shell == want.Shell:
				m.Present = true
			case strings.Contains(x.Command, gateArgs) && x.Command != want.Command:
				m.Others++
			}
		}
	}
	if m.Present {
		return m, nil
	}

	added, err := marshal(h.entry())
	if err != nil {
		return Merge{}, err
	}
	entries = append(entries, added)
	pre, err := marshal(entries)
	if err != nil {
		return Merge{}, err
	}
	hooks = set(hooks, pi, "PreToolUse", pre)
	hooksObj, err := object(hooks)
	if err != nil {
		return Merge{}, err
	}
	top = set(top, hi, "hooks", hooksObj)
	out, err := object(top)
	if err != nil {
		return Merge{}, err
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, out); err != nil {
		return Merge{}, err
	}
	if err := json.Indent(&indented, compact.Bytes(), "", "  "); err != nil {
		return Merge{}, err
	}
	indented.WriteByte('\n')
	m.Data = indented.Bytes()
	return m, nil
}

// Snippet is a settings fragment that registers h, for an administrator to
// merge into Claude Code's managed settings.
func Snippet(h Hook) ([]byte, error) {
	m, err := MergeHook(nil, h)
	if err != nil {
		return nil, err
	}
	return m.Data, nil
}

// WriteSettings replaces the settings file at path with data, atomically: a
// temporary file in the same directory, renamed over it. A symbolic link is
// followed, so the file it names is replaced and the link kept. When the
// file exists, a copy of it is kept first beside it, named
// settings.json.pbipx-backup-<time>; the copy's path is returned. A new
// file and its directory are readable by the user alone.
func WriteSettings(path string, data []byte, now time.Time) (backup string, err error) {
	target := path
	if resolved, rerr := filepath.EvalSymlinks(path); rerr == nil {
		target = resolved
	}
	mode := fs.FileMode(0o600)
	old, rerr := os.ReadFile(target) //nolint:gosec // G304: the settings path is the user's own config file
	switch {
	case rerr == nil:
		if info, serr := os.Stat(target); serr == nil {
			mode = info.Mode().Perm()
		}
		backup = target + ".pbipx-backup-" + now.UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backup, old, 0o600); err != nil { //nolint:gosec // G703: beside the user's own settings file
			return "", err
		}
	case errors.Is(rerr, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
	default:
		return "", rerr
	}
	return backup, writeAtomic(target, data, mode)
}

// writeAtomic writes data to a temporary file beside path and renames it
// over path.
func writeAtomic(path string, data []byte, mode fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".pbipx-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// member is one member of a JSON object, its value as written.
type member struct {
	key   string
	value json.RawMessage
}

// objectMembers splits a JSON object into its members, in order.
func objectMembers(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, ErrSettings
	}
	members := []member{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, ErrSettings
		}
		key, ok := tok.(string)
		if !ok {
			return nil, ErrSettings
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, ErrSettings
		}
		members = append(members, member{key, v})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, ErrSettings
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrSettings
	}
	return members, nil
}

// find is the index of key in members, or -1. A key given twice is an
// error: which one Claude Code reads is not pbipx's to guess.
func find(members []member, key string) (int, error) {
	at := -1
	for i, m := range members {
		if m.key == key {
			if at >= 0 {
				return -1, ErrSettings
			}
			at = i
		}
	}
	return at, nil
}

// set replaces the value at index i, or appends the member when i is -1.
func set(members []member, i int, key string, value json.RawMessage) []member {
	if i >= 0 {
		members[i].value = value
		return members
	}
	return append(members, member{key, value})
}

// object writes members as a JSON object.
func object(members []member) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(m.key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal is json.Marshal without HTML escaping, so a hook command's `&`
// and `>` stay readable.
func marshal(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
