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

// HookEnv are the environment variables a Claude Code settings file may not
// set in its "env", because each can turn the gate's hook off or defeat it:
//
//   - PBIPX_CONFIG_DIR points the gate at another approval store, one that
//     approves whatever policy the repository holds;
//   - CLAUDE_CONFIG_DIR moves the user-level settings the hook is
//     registered in;
//   - PATH changes which programs the hook's shell runs;
//   - BASH_ENV and ENV run a script in every shell, the hook's included;
//   - SHELLOPTS turns shell options on in every bash, errexit among them.
//
// PBIPX_NO_REVEAL is not here: it only takes a power away. Names are
// compared without regard to case, as Windows reads them.
var HookEnv = []string{"PBIPX_CONFIG_DIR", "CLAUDE_CONFIG_DIR", "PATH", "BASH_ENV", "ENV", "SHELLOPTS"}

// HooksDisabled reports whether a Claude Code settings file's content can
// turn the gate's hook off or defeat it: it sets disableAllHooks to anything
// but false, or its "env" sets a variable in HookEnv, or "env" is not a JSON
// object. Content that is not a JSON object fails closed and counts as
// disabling them: pbipx cannot tell what Claude Code would make of it. A
// leading byte order mark is accepted.
func HooksDisabled(data []byte) bool {
	data = bytes.TrimPrefix(data, bom)
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || top == nil {
		return true
	}
	if v, ok := top["disableAllHooks"]; ok && string(bytes.TrimSpace(v)) != "false" {
		return true
	}
	if v, ok := top["env"]; ok {
		var env map[string]json.RawMessage
		if json.Unmarshal(v, &env) != nil || env == nil {
			return true
		}
		for name := range env {
			for _, bad := range HookEnv {
				if strings.EqualFold(name, bad) {
					return true
				}
			}
		}
	}
	return false
}
