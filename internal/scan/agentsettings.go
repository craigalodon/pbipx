package scan

import (
	"bytes"
	"encoding/json"
	"strings"
)

// agentSettingsDir is the directory Claude Code reads a project's settings
// from, and agentSettingsNames the files there that can turn hooks off.
const agentSettingsDir = ".claude"

var agentSettingsNames = []string{"settings.json", "settings.local.json"}

// IsAgentSettings reports whether the repository-relative path rel, with
// forward slashes, is a Claude Code settings file: .claude/settings.json or
// .claude/settings.local.json in any directory. Names are compared without
// regard to case, since a case-insensitive file system reads either
// spelling.
func IsAgentSettings(rel string) bool {
	dir, name, ok := cutLast(rel)
	if !ok {
		return false
	}
	if _, last, ok := cutLast(dir); ok {
		dir = last
	}
	if !strings.EqualFold(dir, agentSettingsDir) {
		return false
	}
	for _, n := range agentSettingsNames {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

// cutLast splits p at its last "/".
func cutLast(p string) (before, after string, ok bool) {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "", p, false
	}
	return p[:i], p[i+1:], true
}

// HooksDisabled reports whether a Claude Code settings file's content can
// turn the gate's hook off: it sets disableAllHooks to anything but false.
// Content that is not a JSON object fails closed and counts as disabling
// them: pbipx cannot tell what Claude Code would make of it. A leading byte
// order mark is accepted.
func HooksDisabled(data []byte) bool {
	data = bytes.TrimPrefix(data, bom)
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || top == nil {
		return true
	}
	v, ok := top["disableAllHooks"]
	if !ok {
		return false
	}
	return string(bytes.TrimSpace(v)) != "false"
}
