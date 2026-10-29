package gate

import (
	"github.com/craigalodon/pbipx/internal/scan"
)

// agentSettingsWhy is why an agent may not change a Claude Code settings
// file: one in the repository can set disableAllHooks, which turns off the
// gate's own hook (threat model, layer 5).
const agentSettingsWhy = "an agent may not change Claude Code's settings in the repository, " +
	"since they can turn off the gate's hook; a person edits them"

// write decides a write of p (behavior.md, "The gate", Write). The gate
// does not otherwise judge writes: only a Claude Code settings file inside
// the repository, under any spelling of p, symlinks resolved, is denied.
func (s *setup) write(p string) Decision {
	if p == "" {
		return allow()
	}
	if rel, ok := s.namesAgentSettings(p); ok {
		return deny(ReasonAgentSettings, "%s is a Claude Code settings file; %s", rel, agentSettingsWhy)
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
