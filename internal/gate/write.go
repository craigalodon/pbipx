package gate

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/scan"
	claudesetup "github.com/craigalodon/pbipx/internal/setup"
)

// agentSettingsWhy is why an agent may not change a Claude Code settings
// file: one in the repository can set disableAllHooks, which turns off the
// gate's own hook (threat model, layer 5).
const agentSettingsWhy = "an agent may not change Claude Code's settings in the repository, " +
	"since they can turn off the gate's hook. " + hintSettings

// controlWhy is why an agent may not write the files outside the
// repository that control the gate: the approval store, where a forged
// record would approve a weakened policy (layer 6), and Claude Code's user
// and managed settings, which register the gate's hook or turn it off
// (layer 5).
const controlWhy = "an agent may not change what controls the gate. " + hintSettings

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
	return loadControls().target(s.spellings(p))
}

// controls are the places outside the repository that control the gate.
type controls struct {
	stores, userDirs, managed []string
}

// loadControls finds the approval store, the user-level Claude Code
// settings directories and the managed settings directory, each as given
// and resolved.
func loadControls() controls {
	var stores []string
	if d, err := approval.ConfigDir(); err == nil {
		stores = append(stores, d)
	}
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		stores = append(stores, filepath.Join(base, "pbipx"))
	}
	return controls{
		stores:   expandAll(stores),
		userDirs: expandAll(claudesetup.UserSettingsDirs()),
		managed:  expandAll([]string{claudesetup.ManagedSettingsDir(runtime.GOOS)}),
	}
}

// target is controlTarget for absolute spellings.
func (c controls) target(spellings []string) (string, bool) {
	for _, a := range expandAll(spellings) {
		for _, d := range c.stores {
			if approval.Within(d, a) {
				return "in the pbipx approval store", true
			}
		}
		for _, d := range c.userDirs {
			if sameDir(d, filepath.Dir(a)) && claudesetup.IsUserSettingsName(filepath.Base(a)) {
				return "a user-level Claude Code settings file", true
			}
		}
		for _, d := range c.managed {
			if approval.Within(d, a) {
				return "in Claude Code's managed settings", true
			}
		}
	}
	return "", false
}

// dir reports whether one of the absolute spellings is a user-level Claude
// Code settings directory: a destination a settings file can be copied or
// moved into under any name.
func (c controls) dir(spellings []string) (string, bool) {
	for _, a := range expandAll(spellings) {
		for _, d := range c.userDirs {
			if sameDir(d, a) {
				return "a user-level Claude Code settings directory", true
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

// claudeConfigDirEnv names Claude Code's configuration directory.
const claudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// controlVar is the value of PBIPX_CONFIG_DIR or CLAUDE_CONFIG_DIR as a
// command's shell would expand it, for the gate's judgment of a path: the
// gate's own environment's value where it is set and usable, and otherwise
// the directory that variable would default to, the store or the user's
// Claude Code settings directory. An agent's shell expands these, so a word
// that names one refers to what controls the gate whether or not the gate's
// environment sets it. ok is false when no directory can be found.
func controlVar(name string) (string, bool) {
	switch name {
	case approval.EnvConfigDir:
		if d, err := approval.ConfigDir(); err == nil {
			return d, true
		}
		if base, err := os.UserConfigDir(); err == nil && base != "" {
			return filepath.Join(base, "pbipx"), true
		}
	case claudeConfigDirEnv:
		if dirs := claudesetup.UserSettingsDirs(); len(dirs) > 0 {
			return dirs[0], true
		}
	}
	return "", false
}

// psControlVar matches a PowerShell reference to PBIPX_CONFIG_DIR or
// CLAUDE_CONFIG_DIR in the environment drive, in any case, braced or not.
var psControlVar = regexp.MustCompile(`(?i)\$(?:\{env:(PBIPX_CONFIG_DIR|CLAUDE_CONFIG_DIR)\}|env:(PBIPX_CONFIG_DIR|CLAUDE_CONFIG_DIR))`)

// expandPSControlVars is w with each such reference replaced by its
// directory (controlVar).
func expandPSControlVars(w string) string {
	return psControlVar.ReplaceAllStringFunc(w, func(m string) string {
		sub := psControlVar.FindStringSubmatch(m)
		name := strings.ToUpper(sub[1] + sub[2])
		if v, ok := controlVar(name); ok {
			return v
		}
		return m
	})
}
