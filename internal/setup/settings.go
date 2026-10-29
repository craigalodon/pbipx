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
	// Present is set when the hook is already registered as it should be:
	// one pbipx entry, with the same matcher, command and shell.
	Present bool
	// Updated is set when an existing registration of the hook was
	// rewritten in place: its matcher or shell brought up to date, or
	// duplicates of it removed.
	Updated bool
	// Others counts the pbipx gate hooks registered with a different
	// command, such as a binary at another path, that are left in place.
	Others int
	// Replaced counts those that were replaced instead (replaceOthers).
	Replaced int
}

// gateHook is one pbipx gate hook found in the PreToolUse entries: the
// entry's index and the hook's index within it.
type gateHook struct {
	entry, hook int
	same        bool // the command is h's: the same binary
}

// preEntry is one PreToolUse entry, split into its members so that it can
// be rewritten keeping every other key. members is nil for an entry in a
// shape pbipx does not read, which is kept as written.
type preEntry struct {
	raw     json.RawMessage
	members []member
	matcher string
	hooks   []json.RawMessage
	removed []bool // hooks to drop
	insert  bool   // the new entry goes right after this one
}

// MergeHook registers h as a PreToolUse hook in existing, the bytes of a
// settings file (empty for none). Every other key, and every hook that is
// not a pbipx gate hook, is kept, in its order. The result is indented with
// two spaces.
//
// A pbipx gate hook is recognised by its command. One for the same command
// as h is upgraded in place: its entry's matcher and its shell are set to
// h's, and any further registrations of it are removed. When its entry
// holds hooks of other tools under another matcher, it is moved out into an
// entry of its own right after, so that their matcher is not changed. A
// gate hook for another binary is counted in Others and left alone, unless
// replaceOthers is set, when it is treated as h's and replaced.
func MergeHook(existing []byte, h Hook, replaceOthers bool) (Merge, error) {
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
	var raws []json.RawMessage
	if pi >= 0 {
		if err := json.Unmarshal(hooks[pi].value, &raws); err != nil {
			return Merge{}, ErrSettings
		}
	}

	want := h.entry().Hooks[0]
	entries := make([]preEntry, len(raws))
	var found []gateHook
	var m Merge
	for i, raw := range raws {
		entries[i] = readEntry(raw)
		e := &entries[i]
		for j, hr := range e.hooks {
			var x hookJSON
			if json.Unmarshal(hr, &x) != nil || !strings.Contains(x.Command, gateArgs) {
				continue
			}
			same := x.Command == want.Command
			switch {
			case same:
				found = append(found, gateHook{i, j, true})
			case replaceOthers:
				found = append(found, gateHook{i, j, false})
				m.Replaced++
			default:
				m.Others++
			}
		}
	}

	var added json.RawMessage
	switch {
	case len(found) == 0:
		if added, err = marshal(h.entry()); err != nil {
			return Merge{}, err
		}
	case len(found) == 1 && found[0].same && upToDate(entries[found[0].entry], found[0].hook, want):
		m.Present = true
		return m, nil
	default:
		m.Updated = true
		first := found[0]
		for _, g := range found[1:] {
			entries[g.entry].removed[g.hook] = true
		}
		e := &entries[first.entry]
		if e.matcher == HookMatcher || e.alone(first.hook) {
			// Rewrite the hook, and the matcher when only removed hooks
			// share it.
			if e.hooks[first.hook], err = rewriteHook(e.hooks[first.hook], want); err != nil {
				return Merge{}, err
			}
			e.matcher = HookMatcher
		} else {
			// Other tools' hooks keep their matcher; the gate's moves out.
			e.removed[first.hook] = true
			e.insert = true
			if added, err = marshal(h.entry()); err != nil {
				return Merge{}, err
			}
		}
	}

	var out []json.RawMessage
	for i := range entries {
		e := &entries[i]
		raw, keep, err := e.render()
		if err != nil {
			return Merge{}, err
		}
		if keep {
			out = append(out, raw)
		}
		if e.insert {
			out = append(out, added)
		}
	}
	if len(found) == 0 {
		out = append(out, added)
	}
	if out == nil {
		out = []json.RawMessage{}
	}

	pre, err := marshal(out)
	if err != nil {
		return Merge{}, err
	}
	hooks = set(hooks, pi, "PreToolUse", pre)
	hooksObj, err := object(hooks)
	if err != nil {
		return Merge{}, err
	}
	top = set(top, hi, "hooks", hooksObj)
	doc, err := object(top)
	if err != nil {
		return Merge{}, err
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, doc); err != nil {
		return Merge{}, err
	}
	if err := json.Indent(&indented, compact.Bytes(), "", "  "); err != nil {
		return Merge{}, err
	}
	indented.WriteByte('\n')
	m.Data = indented.Bytes()
	return m, nil
}

// readEntry splits a PreToolUse entry. An entry that is not an object with
// a string matcher (or none) and an array of hooks is kept as written.
func readEntry(raw json.RawMessage) preEntry {
	e := preEntry{raw: raw}
	members, err := objectMembers(raw)
	if err != nil {
		return e
	}
	mi, err1 := find(members, "matcher")
	hk, err2 := find(members, "hooks")
	if err1 != nil || err2 != nil || hk < 0 {
		return e
	}
	if mi >= 0 && json.Unmarshal(members[mi].value, &e.matcher) != nil {
		return e
	}
	if json.Unmarshal(members[hk].value, &e.hooks) != nil {
		e.hooks = nil
		return e
	}
	e.members = members
	e.removed = make([]bool, len(e.hooks))
	return e
}

// alone reports whether hook i is the only hook of the entry left.
func (e *preEntry) alone(i int) bool {
	for j := range e.hooks {
		if j != i && !e.removed[j] {
			return false
		}
	}
	return true
}

// render is the entry as it now stands; keep is false when every hook was
// removed from it.
func (e *preEntry) render() (raw json.RawMessage, keep bool, err error) {
	if e.members == nil {
		return e.raw, true, nil
	}
	var hooks []json.RawMessage
	changed := false
	for j, hr := range e.hooks {
		if e.removed[j] {
			changed = true
			continue
		}
		hooks = append(hooks, hr)
	}
	if len(hooks) == 0 && changed {
		return nil, false, nil
	}
	members := e.members
	if mi, _ := find(members, "matcher"); mi >= 0 || e.matcher != "" {
		v, err := marshal(e.matcher)
		if err != nil {
			return nil, false, err
		}
		members = set(members, mi, "matcher", v)
	}
	if hooks == nil {
		hooks = []json.RawMessage{}
	}
	hv, err := marshal(hooks)
	if err != nil {
		return nil, false, err
	}
	hk, _ := find(members, "hooks")
	members = set(members, hk, "hooks", hv)
	raw, err = object(members)
	return raw, true, err
}

// upToDate reports whether hook i of e is want, under the current matcher.
func upToDate(e preEntry, i int, want hookJSON) bool {
	var x hookJSON
	if json.Unmarshal(e.hooks[i], &x) != nil {
		return false
	}
	return e.matcher == HookMatcher && x.Type == want.Type && x.Command == want.Command && x.Shell == want.Shell
}

// rewriteHook sets a hook's type, command and shell to want's, keeping its
// other keys. An empty shell removes the key.
func rewriteHook(raw json.RawMessage, want hookJSON) (json.RawMessage, error) {
	members, err := objectMembers(raw)
	if err != nil {
		return nil, err
	}
	for _, kv := range [][2]string{{"type", want.Type}, {"command", want.Command}, {"shell", want.Shell}} {
		i, err := find(members, kv[0])
		if err != nil {
			return nil, err
		}
		if kv[1] == "" {
			if i >= 0 {
				members = append(members[:i], members[i+1:]...)
			}
			continue
		}
		v, err := marshal(kv[1])
		if err != nil {
			return nil, err
		}
		members = set(members, i, kv[0], v)
	}
	return object(members)
}

// Snippet is a settings fragment that registers h, for an administrator to
// merge into Claude Code's managed settings.
func Snippet(h Hook) ([]byte, error) {
	m, err := MergeHook(nil, h, false)
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
