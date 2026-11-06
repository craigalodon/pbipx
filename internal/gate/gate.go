// Package gate decides whether an AI agent's tool call may run
// (docs/design/behavior.md, "The gate (Claude Code)"; principles 7 and 8).
// It knows nothing of any one agent's hook protocol: an adapter turns the
// agent's event into an Event and the Decision into the agent's reply.
//
// The gate acts for adopted repositories only (adopt.go): a call is judged
// against each one it touches, wherever it is made from, and is otherwise
// allowed. Every failure is a deny. An adopted repository's policy and
// baseline are loaded before the tool is looked at, so a broken setup
// denies every call that touches it. A Decision's Message names files,
// columns, reasons and classes only: never a value, never source text,
// never the command line the agent sent.
package gate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/craigalodon/pbipx/internal/baseline"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/source"
)

// Tool is the kind of tool an agent is about to call.
type Tool string

// Tools. Any other tool, the empty one included, is decided as Other.
const (
	Read   Tool = "read"   // reads one file
	Search Tool = "search" // searches the files under a path
	Shell  Tool = "shell"  // runs a shell command line
	Write  Tool = "write"  // creates or changes one file
	// Edit changes one file and shows the model a snippet of it around the
	// change (Edit, MultiEdit, NotebookEdit): judged as a Read of the file
	// and as a Write.
	Edit Tool = "edit"
	// PowerShell runs a PowerShell command line, which the gate cannot
	// parse (powershell.go).
	PowerShell Tool = "powershell"
	// LSP asks a language server about the files at Paths, or about the
	// working directory's project when there are none (powershell.go).
	LSP   Tool = "lsp"
	Other Tool = "other" // anything else: allowed once the setup loads
)

// Event is one tool call, in the agent's terms translated.
type Event struct {
	Tool Tool
	// Path is the file a read or a write names, or the path a search
	// covers (empty for the working directory), as the agent gave it: it
	// may be relative to Cwd.
	Path string
	// Paths are the files an LSP call names, as the agent gave them.
	Paths []string
	// Command is a shell or PowerShell call's command line.
	Command string
	// Cwd is the agent's working directory, absolute.
	Cwd string
}

// Decision is the gate's answer to one Event.
type Decision struct {
	Allow bool
	// Reason is a gate-reasons class from the corpus vocabulary
	// (testdata/corpus/vocabulary.toml) when Allow is false, and "" on an
	// allow.
	Reason string
	// Message is for the agent: what was denied and why, in structure
	// only. It does not repeat Reason; an adapter writes the two as
	// "pbipx: <Reason>: <Message>". Empty on an allow.
	Message string
}

// Options configures Decide.
type Options struct {
	// CheckPolicy, if set, runs after the policy loads and before anything
	// else is decided, with the repository root and the exact bytes the
	// policy was parsed from: the file is read once, so the policy checked is
	// the policy enforced. A non-nil Decision it returns is final. ADR-0006's
	// approval check plugs in here.
	CheckPolicy func(root string, policyData []byte) *Decision

	// Adoption, if set, scopes the gate to adopted repositories (adopt.go;
	// ADR-0006, amended 2026-10-30): a call is judged against each
	// adopted repository it touches, wherever it is made from, and is
	// otherwise allowed. Nil judges every call against the repository the
	// working directory is in, and denies one made outside any repository.
	Adoption *Adoption

	// Now is when baseline expiry is judged; zero means time.Now().
	Now time.Time

	// msys places the Git Bash paths of one decision (msys.go); Decide
	// sets it.
	msys *msysSession
}

// Reason classes (vocabulary.toml, [gate-reasons]).
const (
	ReasonFinding          = "finding"
	ReasonUnvettable       = "unvettable"
	ReasonScanError        = "scan-error"
	ReasonLocalState       = "local-state"
	ReasonPerfExport       = "perf-export"
	ReasonAgentSettings    = "agent-settings"
	ReasonGitContent       = "git-content"
	ReasonWorkingCopy      = "working-copy"
	ReasonPolicyError      = "policy-error"
	ReasonPolicyUnapproved = "policy-unapproved"
	ReasonEventError       = "event-error"
	ReasonNotARepository   = "not-a-repository"
	ReasonInternalError    = "internal-error"
)

// Reasons lists every reason class.
var Reasons = []string{
	ReasonFinding, ReasonUnvettable, ReasonScanError, ReasonLocalState, ReasonPerfExport, ReasonAgentSettings, ReasonGitContent, ReasonWorkingCopy,
	ReasonPolicyError, ReasonPolicyUnapproved, ReasonEventError, ReasonNotARepository, ReasonInternalError,
}

func allow() Decision { return Decision{Allow: true} }

func deny(reason, format string, args ...any) Decision {
	return Decision{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// setup is what every decision needs: the repository, its policy and its
// baseline.
type setup struct {
	repo     *source.Repo
	policy   *policy.Policy
	baseline *baseline.Baseline
	cwd      string
	now      time.Time
	// perf is what this decision has spent on Performance Analyzer shape
	// checks of files a command names (perfexport.go).
	perf perfBudget
	// fold is whether paths are compared without regard to case
	// (foldCase); home is the gate's own home directory, "" when unknown.
	fold bool
	home string
	// gitq is the decision's git session (gitquery.go).
	gitq *gitSession
	// nest is what is known of nested work trees (nested.go), found on
	// first use.
	nest *nestCache
	// hooks is whether the repository has a hook that runs on git
	// commands that change nothing a later one prints (indexHooks), known
	// once hooksKnown.
	hooks, hooksKnown bool
	// ctlVars is whether the call's line provably does not set
	// PBIPX_CONFIG_DIR or CLAUDE_CONFIG_DIR, so a word naming one is
	// expanded from the gate's environment (ctlVarsFixed).
	ctlVars bool
	// cdpath is whether the call's line may set CDPATH, or the gate's own
	// environment has it: cd then takes a bare name from a folder the gate
	// cannot tell (cdpathFixed).
	cdpath bool
	// msys places the absolute Git Bash paths that are not drive paths,
	// nil off Windows (msys.go).
	msys *msysSession
}

// Decide decides one tool call. It never panics: a panic below it is a deny
// with internal-error.
func Decide(ev Event, opts Options) (d Decision) {
	defer func() {
		if recover() != nil {
			d = deny(ReasonInternalError, "the gate failed; the call is denied")
		}
		if !d.Allow && opts.msys.notFound() {
			d.Message += " " + msysNotFoundNote
		}
	}()
	if opts.msys == nil {
		opts.msys = newMSYSSession()
		opts.msys.addRoot(ev.Cwd)
		opts.msys.addRepoOf(ev.Cwd)
	}
	if opts.Adoption != nil {
		return decideAdopted(ev, opts)
	}
	s, failed := load(ev, opts)
	if failed != nil {
		return *failed
	}
	return s.decide(ev)
}

// decide decides ev against the setup's repository. A file tool's path in
// Git Bash form (/tmp/x) is judged as written and as placed (msys.go); one
// that cannot be placed is denied.
func (s *setup) decide(ev Event) Decision {
	alt, ok := s.placedEvent(ev)
	if !ok {
		return deny(ReasonInternalError, "a path in Git Bash form could not be placed by Git Bash's cygpath; the call is denied")
	}
	if alt != nil {
		if d := s.decideAs(*alt); !d.Allow {
			return d
		}
	}
	return s.decideAs(ev)
}

// placedEvent is ev with each path in Git Bash form placed, nil when no
// path needs it; ok is false when one cannot be placed.
func (s *setup) placedEvent(ev Event) (alt *Event, ok bool) {
	if s.msys == nil {
		return nil, true
	}
	switch ev.Tool {
	case Read, Search, Write, Edit:
		pl, needed, ok := s.msys.word(ev.Path)
		if !ok {
			return nil, false
		}
		if !needed {
			return nil, true
		}
		a := ev
		a.Path = pl
		return &a, true
	case LSP:
		a := ev
		a.Paths = slices.Clone(ev.Paths)
		changed := false
		for i, p := range ev.Paths {
			pl, needed, ok := s.msys.word(p)
			if !ok {
				return nil, false
			}
			if needed {
				a.Paths[i], changed = pl, true
			}
		}
		if changed {
			return &a, true
		}
	}
	return nil, true
}

// decideAs decides ev as written.
func (s *setup) decideAs(ev Event) Decision {
	switch ev.Tool {
	case Read:
		return s.read(ev.Path)
	case Search:
		return s.search(ev.Path)
	case Shell:
		return s.shell(ev.Command)
	case Write:
		return s.write(ev.Path)
	case Edit:
		// The result shows the file around the edit: a read.
		if d := s.read(ev.Path); !d.Allow {
			return d
		}
		return s.write(ev.Path)
	case PowerShell:
		return s.powershell(ev.Command)
	case LSP:
		return s.lsp(ev.Paths)
	}
	return allow()
}

// load finds the repository the working directory is in and loads its
// policy, then runs opts.CheckPolicy, then loads the baseline. Any failure
// is the returned Decision.
//
// A baseline that fails to load denies with policy-error: the vocabulary
// has no class of its own for it, and like the policy it is the
// repository's reviewed acceptance configuration, loaded before any tool is
// looked at, so a broken one denies every call.
func load(ev Event, opts Options) (*setup, *Decision) {
	fail := func(d Decision) (*setup, *Decision) { return nil, &d }
	if ev.Cwd == "" || !filepath.IsAbs(ev.Cwd) {
		return fail(deny(ReasonEventError, "the event has no absolute working directory"))
	}
	if info, err := os.Stat(ev.Cwd); err != nil || !info.IsDir() {
		return fail(deny(ReasonNotARepository, "the working directory does not exist"))
	}
	repo, err := source.Open(ev.Cwd)
	if err != nil {
		if errors.Is(err, source.ErrNotARepository) {
			return fail(deny(ReasonNotARepository, "the working directory is not in a git repository"))
		}
		return fail(deny(ReasonInternalError, "git could not say which repository the working directory is in"))
	}
	return loadAt(ev, opts, repo)
}

// loadAt loads the policy of the repository repo, runs opts.CheckPolicy,
// then loads the baseline, for ev judged as if made in repo, from ev.Cwd
// wherever that is.
func loadAt(ev Event, opts Options, repo *source.Repo) (*setup, *Decision) {
	fail := func(d Decision) (*setup, *Decision) { return nil, &d }
	policyPath := filepath.Join(repo.Root, policy.FileName)
	p, policyData, err := policy.LoadData(policyPath)
	if err != nil {
		var pe *policy.Error
		if errors.As(err, &pe) {
			return fail(deny(ReasonPolicyError, "%s", describe(policy.FileName, pe.Line, pe.Key, pe.Msg)))
		}
		return fail(deny(ReasonPolicyError, "%s cannot be loaded", policy.FileName))
	}
	if opts.CheckPolicy != nil {
		if d := opts.CheckPolicy(repo.Root, policyData); d != nil {
			return fail(*d)
		}
	}
	b, err := baseline.Load(filepath.Join(repo.Root, baseline.FileName))
	if err != nil {
		var be *baseline.Error
		if errors.As(err, &be) {
			key := be.Key
			if be.Entry != "" {
				key = strings.TrimSpace("entry " + be.Entry + " " + key)
			}
			return fail(deny(ReasonPolicyError, "%s", describe(baseline.FileName, be.Line, key, be.Msg)))
		}
		return fail(deny(ReasonPolicyError, "%s cannot be loaded", baseline.FileName))
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	opts.msys.addRoot(repo.Root)
	return &setup{repo: repo, policy: p, baseline: b, cwd: ev.Cwd, now: now, fold: foldCase, home: userHome(), gitq: newGitSession(), msys: opts.msys}, nil
}

// userHome is the directory "~" and $HOME stand for, "" when unknown. On
// Windows Git Bash and Claude Code both take $HOME first, which may be in
// a Git Bash form (hostPath), and %USERPROFILE% (os.UserHomeDir) when it
// is not set; elsewhere os.UserHomeDir is $HOME.
func userHome() string {
	if windowsPaths {
		if h := os.Getenv("HOME"); h != "" && fullPath(h) {
			return filepath.Clean(hostPath(h))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// describe is a policy or baseline error as structure: the file's name, a
// line, a key and what is wrong. Both packages' errors already hold no value.
func describe(name string, line int, key, msg string) string {
	s := name
	if line > 0 {
		s += fmt.Sprintf(":%d", line)
	}
	s += ": "
	if key != "" {
		s += key + " "
	}
	return s + msg
}
