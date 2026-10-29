package gate

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/scan"
	claudesetup "github.com/craigalodon/pbipx/internal/setup"
)

// agentSettingsWhy is why an agent may not change a Claude Code settings
// file: one in the repository can set disableAllHooks, which turns off the
// gate's own hook (threat model, layer 5).
const agentSettingsWhy = "an agent may not change Claude Code's settings in the repository, " +
	"since they can turn off the gate's hook; a person edits them"

// controlWhy is why an agent may not write the files outside the
// repository that control the gate: the approval store, where a forged
// record would approve a weakened policy (layer 6), and Claude Code's user
// and managed settings, which register the gate's hook or turn it off
// (layer 5).
const controlWhy = "an agent may not change what controls the gate; a person does, at a terminal"

// write decides a write of p (behavior.md, "The gate", Write). The gate
// does not otherwise judge writes: only a Claude Code settings file inside
// the repository, and the files outside it that control the gate
// (controlTarget), under any spelling of p, symlinks resolved, are denied.
func (s *setup) write(p string) Decision {
	if p == "" {
		return allow()
	}
	if rel, ok := s.namesAgentSettings(expandHome(p, s.home)); ok {
		return deny(ReasonAgentSettings, "%s is a Claude Code settings file; %s", displayPath(rel), agentSettingsWhy)
	}
	if what, ok := s.controlTarget(p); ok {
		return deny(ReasonAgentSettings, "the path is %s; %s", what, controlWhy)
	}
	return allow()
}

// namesAgentSettings reports whether p, relative to the working directory
// or absolute, is a Claude Code settings file inside the repository under
// any of its spellings. It returns the path, repository-relative.
func (s *setup) namesAgentSettings(p string) (string, bool) {
	for _, rel := range s.rels(p) {
		if scan.IsAgentSettings(rel) {
			return rel, true
		}
	}
	return "", false
}

// controlTarget reports whether p, under any of its spellings, is inside
// the pbipx approval store (approval.ConfigDir, and the default
// <user config dir>/pbipx), is a user-level Claude Code settings file
// (settings*.json directly in $CLAUDE_CONFIG_DIR or ~/.claude), or is
// inside the managed settings directory. It returns what the path is, in
// words: the message never names a path outside the repository.
func (s *setup) controlTarget(p string) (string, bool) {
	var stores []string
	if d, err := approval.ConfigDir(); err == nil {
		stores = append(stores, d)
	}
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		stores = append(stores, filepath.Join(base, "pbipx"))
	}
	stores = expandAll(stores)
	userDirs := expandAll(claudesetup.UserSettingsDirs())
	managed := expandAll([]string{claudesetup.ManagedSettingsDir(runtime.GOOS)})
	for _, a := range expandAll(s.spellings(p)) {
		for _, d := range stores {
			if approval.Within(d, a) {
				return "in the pbipx approval store", true
			}
		}
		for _, d := range userDirs {
			if sameDir(d, filepath.Dir(a)) && claudesetup.IsUserSettingsName(filepath.Base(a)) {
				return "a user-level Claude Code settings file", true
			}
		}
		for _, d := range managed {
			if approval.Within(d, a) {
				return "in Claude Code's managed settings", true
			}
		}
	}
	return "", false
}

// sameDir reports whether a and b, absolute and clean, name the same
// directory (approval.Within both ways, so case is compared as it is
// there).
func sameDir(a, b string) bool { return approval.Within(a, b) && approval.Within(b, a) }

// expandAll is each directory as given and with the symlinks of its deepest
// existing ancestor resolved.
func expandAll(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		out = append(out, filepath.Clean(d), approval.ResolveExisting(d))
	}
	return out
}
