package gate

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/source"
)

// Adoption scopes the gate to the repositories that adopted pbipx
// (behavior.md, "The gate", adoption; ADR-0006, amended 2026-10-30). The
// gate is registered in the user's settings, so it runs in every session
// on the machine, in every directory: it acts only where pbipx was set up.
//
// A repository is adopted when its work tree has a .pbipx.toml, or its
// clone has a record in the approval store (written by `pbipx init` or
// `pbipx policy approve`), or its work tree is in the store's index of
// adopted work trees. The record and the index live outside the
// repository, so deleting or breaking the policy cannot switch the gate
// off once pbipx was set up.
type Adoption struct {
	// Store holds the records and the index. Nil is the store in
	// approval.ConfigDir, found on each decision; when there is none, a
	// repository is adopted by its .pbipx.toml alone.
	Store *approval.Store
}

func (a *Adoption) store() *approval.Store {
	if a.Store != nil {
		return a.Store
	}
	s, err := approval.Open()
	if err != nil {
		return nil
	}
	return s
}

// spot is a path a call touches, absolute. A container spot also touches
// every adopted work tree below it: a search, or a command's word, can
// reach what is under a directory. The working directory alone does not.
//
// Two flags narrow what a spot means to a registered working copy
// (workingcopy.go), which is denied with no scan. bare: the call names the
// path and reads nothing under it (a lister's word, a cd's target, a
// PowerShell call's working directory), so a working copy below it is not
// reached; a working copy at or under the path still is. prose: the word
// is the text of a git message, no path.
type spot struct {
	path      string
	container bool
	bare      bool
	prose     bool
	// only: for working copies alone; the adoption finder ignores it.
	only bool
	// all: for working copies alone; the call moves or links a path it
	// cannot tell, which could be a folder above any copy.
	all bool
	// reads: for working copies alone; the call reads the path (a Read's or
	// a Grep's, a word of a program not known to leave its words unread).
	reads bool
}

// finder finds the adopted repositories one call touches.
type finder struct {
	ev    Event
	store *approval.Store
	// index is every indexed work tree, as recorded and with its symlinks
	// resolved; records is whether the store holds any record.
	index   []string
	records bool
	home    string
	fold    bool
	gitAt   map[string]bool
	adopted map[string]bool
	// probe is the walk of a shell call's line (probeShell).
	probe *walker
	// cwdErr is why git cannot open the working directory, nil when it
	// can; found on first use (cwdUsable).
	cwdOpened bool
	cwdErr    error
	// msys places Git Bash paths (msys.go), nil off Windows.
	msys *msysSession
	// found is the result of spots, once spotsDone.
	found     []spot
	foundUnk  bool
	spotsDone bool
}

// newFinder is the finder for ev over store (nil: none), with the store's
// index read. An index that cannot be read is an error.
func newFinder(ev Event, store *approval.Store) (*finder, error) {
	f := &finder{ev: ev, store: store, home: userHome(), fold: foldCase,
		gitAt: map[string]bool{}, adopted: map[string]bool{}}
	if f.store != nil {
		roots, records, err := f.store.Roots()
		if err != nil {
			return nil, err
		}
		f.records = records
		for _, r := range roots {
			f.index = append(f.index, r, resolve(r))
			// Every linked work tree of the clone counts too: a search
			// from a common parent reaches a sibling git worktree add made.
			for _, wt := range linkedWorktrees(r) {
				f.index = append(f.index, wt, resolve(wt))
			}
		}
		slices.Sort(f.index)
		f.index = slices.Compact(f.index)
	}
	return f, nil
}

// decideAdopted decides ev under opts.Adoption: against each adopted
// repository it touches (targets), any deny winning, and then the files
// outside every repository that control the gate (controls). A call that
// touches no adopted repository and no such file is allowed.
func decideAdopted(ev Event, opts Options) Decision {
	if ev.Cwd == "" || !filepath.IsAbs(ev.Cwd) {
		return deny(ReasonEventError, "the event has no absolute working directory")
	}
	f, err := newFinder(ev, opts.Adoption.store())
	if err != nil {
		return deny(ReasonInternalError, "the index of repositories set up for pbipx cannot be read; the call is denied")
	}
	f.msys = opts.msys
	if !f.placeable() {
		return deny(ReasonInternalError, "a path in Git Bash form could not be placed by Git Bash's cygpath; the call is denied")
	}
	if d := f.workingCopyDeny(); d != nil {
		return *d
	}
	targets, failed := f.targets()
	if failed != nil {
		return *failed
	}
	for _, repo := range targets {
		s, failed := loadAt(ev, opts, repo)
		if failed != nil {
			return *failed
		}
		if d := s.decide(ev); !d.Allow {
			return d
		}
	}
	return f.controls()
}

// targets are the adopted repositories ev touches. For each path the call
// touches (spots), as given and with its symlinks resolved, that is the
// innermost adopted work tree holding it: of the directories above it
// that hold a .git (walking up), and the indexed work trees it is inside.
// An adopted work tree nested in another is judged alone, as the
// repository it is (a linked work tree of the clone under
// .claude/worktrees, say), and a repository that has not adopted pbipx,
// nested in one that has, leaves the path to the one that has. A
// container spot also touches every indexed work tree below it, and a
// call that touches something that cannot be placed touches every indexed
// work tree. The ones that hold the working directory come first,
// innermost first. An adopted repository that git cannot open as a work
// tree is a deny.
func (f *finder) targets() ([]*source.Repo, *Decision) {
	spots, unknown := f.spots()
	var cands []string
	add := func(c string) {
		if !slices.Contains(cands, c) {
			cands = append(cands, c)
		}
	}
	for _, sp := range spots {
		if sp.only {
			continue
		}
		for _, a := range []string{sp.path, resolve(sp.path)} {
			var holders []string
			for d := a; ; d = filepath.Dir(d) {
				if f.hasGit(d) {
					holders = append(holders, d)
				}
				if filepath.Dir(d) == d {
					break
				}
			}
			for _, r := range f.index {
				if _, in := relTo(r, a, f.fold); in {
					holders = append(holders, r)
				} else if _, holds := relTo(a, r, f.fold); holds && sp.container {
					add(r)
				}
			}
			slices.SortStableFunc(holders, func(x, y string) int { return len(y) - len(x) })
			if i := slices.IndexFunc(holders, f.inReach); i >= 0 {
				add(holders[i])
			}
		}
	}
	if unknown {
		for _, r := range f.index {
			add(r)
		}
	}
	// The working directory's repositories first, innermost first; then
	// the rest in order.
	holdsCwd := func(c string) bool {
		_, in := relTo(c, f.ev.Cwd, f.fold)
		_, inResolved := relTo(c, resolve(f.ev.Cwd), f.fold)
		return in || inResolved
	}
	slices.SortStableFunc(cands, func(a, b string) int {
		ha, hb := holdsCwd(a), holdsCwd(b)
		switch {
		case ha && !hb:
			return -1
		case hb && !ha:
			return 1
		case ha && hb:
			return len(b) - len(a)
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})

	var out []*source.Repo
	for _, c := range cands {
		if !f.inReach(c) {
			continue
		}
		if holdsCwd(c) {
			if d := f.cwdUsable(); d != nil {
				return nil, d
			}
		}
		repo, err := source.Open(c)
		if err != nil || !samePath(resolve(c), repo.Root, f.fold) {
			d := deny(ReasonNotARepository, "the call touches a repository set up for pbipx (pbipx init, or an approved policy) "+
				"that git cannot open as a work tree; the call is denied")
			return nil, &d
		}
		if !slices.ContainsFunc(out, func(r *source.Repo) bool { return r.Root == repo.Root }) {
			out = append(out, repo)
		}
	}
	return out, nil
}

// cwdUsable is nil when git opens the working directory as a work tree,
// and otherwise the deny for a working directory inside an adopted
// repository that git cannot place: it does not exist, or is not in a
// work tree (inside the git directory, say).
func (f *finder) cwdUsable() *Decision {
	if !f.cwdOpened {
		f.cwdOpened = true
		if info, err := os.Stat(f.ev.Cwd); err != nil || !info.IsDir() {
			f.cwdErr = errNoCwd
		} else if _, err := source.Open(f.ev.Cwd); err != nil {
			f.cwdErr = err
		}
	}
	var d Decision
	switch {
	case f.cwdErr == nil:
		return nil
	case errors.Is(f.cwdErr, errNoCwd):
		d = deny(ReasonNotARepository, "the working directory does not exist")
	case errors.Is(f.cwdErr, source.ErrNotARepository):
		d = deny(ReasonNotARepository, "the working directory is in a repository set up for pbipx, and git cannot place it in a work tree")
	default:
		d = deny(ReasonInternalError, "git could not say which repository the working directory is in")
	}
	return &d
}

// errNoCwd is a working directory that does not exist.
var errNoCwd = errors.New("the working directory does not exist")

// inReach reports whether the work tree at root is the gate's to judge: it
// exists, and is adopted. An indexed work tree that is gone holds nothing
// to protect.
func (f *finder) inReach(root string) bool {
	if _, err := os.Lstat(root); err != nil {
		return false
	}
	return f.isAdopted(root)
}

// hasGit reports whether dir holds a .git entry: a directory, or the file
// a linked work tree or a submodule has.
func (f *finder) hasGit(dir string) bool {
	if r, ok := f.gitAt[dir]; ok {
		return r
	}
	_, err := os.Lstat(filepath.Join(dir, gitEntry))
	f.gitAt[dir] = err == nil
	return err == nil
}

// isAdopted reports whether the work tree at root is adopted: it has a
// .pbipx.toml (of any kind: a broken one is a policy-error), it is in the
// index, or its clone has a record. A record that cannot be read counts.
func (f *finder) isAdopted(root string) bool {
	if r, ok := f.adopted[root]; ok {
		return r
	}
	r := f.adoptedNow(root)
	f.adopted[root] = r
	return r
}

func (f *finder) adoptedNow(root string) bool { return f.adoptedWhy(root) != "" }

// adoptedWhy is why the work tree at root is adopted (an AdoptedBy kind),
// or "" when it is not.
func (f *finder) adoptedWhy(root string) string {
	if _, err := os.Lstat(filepath.Join(root, policy.FileName)); err == nil {
		return ByPolicyFile
	}
	for _, a := range []string{root, resolve(root)} {
		if slices.ContainsFunc(f.index, func(r string) bool { return samePath(r, a, f.fold) }) {
			return ByIndex
		}
	}
	if !f.records {
		return ""
	}
	id, err := Identify(root)
	if err != nil {
		// git cannot name the clone, so no record can be found for it.
		return ""
	}
	ok, err := f.store.Adopted(id)
	if ok || err != nil {
		return ByRecord
	}
	return ""
}

// Why a work tree is adopted (AdoptedBy.Kind).
const (
	// ByPolicyFile: the work tree has a .pbipx.toml.
	ByPolicyFile = "policy-file"
	// ByIndex: the work tree is in the store's index of adopted work trees.
	ByIndex = "index"
	// ByRecord: the work tree's clone has a record in the store, one that
	// cannot be read included.
	ByRecord = "record"
	// ByEnclosing: the work tree lies inside another adopted work tree,
	// which the gate judges its paths against.
	ByEnclosing = "enclosing"
	// ByUnplaceable: git cannot open an adopted work tree the work tree
	// lies in, and the gate denies there.
	ByUnplaceable = "unplaceable"
)

// AdoptedBy says why the gate acts for a call made in a work tree.
type AdoptedBy struct {
	// Kind is one of the By constants.
	Kind string
	// Root is the adopted work tree that holds the call: the work tree
	// itself or, for ByEnclosing, the one around it. Empty for
	// ByUnplaceable.
	Root string
}

// AdoptedBy is nil when the gate does not act for a call made in the work
// tree at root: it is not adopted, and no adopted work tree holds it.
// It is the adoption the gate decides by, so a caller that removes an
// adoption can ask whether it is gone. An index that cannot be read is an
// error.
func (a *Adoption) AdoptedBy(root string) (*AdoptedBy, error) {
	f, err := newFinder(Event{Tool: Read, Cwd: root, Path: root}, a.store())
	if err != nil {
		return nil, err
	}
	targets, failed := f.targets()
	if failed != nil {
		return &AdoptedBy{Kind: ByUnplaceable}, nil
	}
	if len(targets) == 0 {
		return nil, nil
	}
	repo := targets[0]
	if samePath(resolve(repo.Root), resolve(root), f.fold) {
		return &AdoptedBy{Kind: f.adoptedWhy(repo.Root), Root: repo.Root}, nil
	}
	return &AdoptedBy{Kind: ByEnclosing, Root: repo.Root}, nil
}

// WorkTrees are the work trees of the clone that the work tree at root
// belongs to, as git worktree list reports them, root first, read without
// running git (linkedWorktrees).
func WorkTrees(root string) []string {
	return append([]string{root}, linkedWorktrees(root)...)
}

// spots are the paths ev touches, and whether it touches something that
// cannot be placed: the working directory; a read's or a write's file; a
// search's path; an LSP call's paths; for a shell call, every working
// directory a command runs in and every word placed against it
// (probeShell); and for a PowerShell call, the working directory and every
// word that looks like a path (probePowerShell).
func (f *finder) spots() ([]spot, bool) {
	if !f.spotsDone {
		f.found, f.foundUnk = f.findSpots()
		f.spotsDone = true
	}
	return f.found, f.foundUnk
}

func (f *finder) findSpots() (spots []spot, unknown bool) {
	ev := f.ev
	addAll := func(paths []string, container bool) {
		for _, p := range paths {
			spots = append(spots, spot{path: p, container: container})
		}
	}
	addAll(spellings(ev.Cwd, "."), false)
	switch ev.Tool {
	case Read, Write, Edit:
		if ev.Path != "" {
			addAll(f.spellings(expandHome(ev.Path, f.home)), false)
		}
	case Search:
		p := ev.Path
		if p == "" {
			p = ev.Cwd
		}
		addAll(f.spellings(expandHome(p, f.home)), true)
	case Shell:
		var probed []spot
		f.probe, probed, unknown = probeShell(ev.Cwd, f.home, ev.Command, f.msys)
		spots = append(spots, probed...)
	case PowerShell:
		var probed []spot
		probed, unknown = probePowerShell(ev.Cwd, f.home, ev.Command)
		spots = append(spots, probed...)
	case LSP:
		for _, p := range ev.Paths {
			if p != "" {
				addAll(f.spellings(expandHome(p, f.home)), true)
			}
		}
	}
	if ev.Tool == Read || ev.Tool == Search {
		for i := range spots {
			spots[i].reads = true
		}
	}
	return spots, unknown
}

// spellings are the spellings of p against the call's working directory,
// and as placed when p is an absolute Git Bash path (msys.go).
func (f *finder) spellings(p string) []string {
	out := spellings(f.ev.Cwd, p)
	if pl, needed, ok := f.msys.word(p); needed && ok {
		out = append(out, spellings(f.ev.Cwd, pl)...)
	}
	return out
}

// placeable reports whether every path a file tool names that is in Git
// Bash form can be placed.
func (f *finder) placeable() bool {
	var paths []string
	switch f.ev.Tool {
	case Read, Write, Edit, Search:
		paths = []string{f.ev.Path}
	case LSP:
		paths = f.ev.Paths
	}
	for _, p := range paths {
		if _, _, ok := f.msys.word(p); !ok {
			return false
		}
	}
	return true
}

// probeShell walks a shell line as the shell rule does, with no repository,
// judging nothing: the walker that read it, the paths it touches (each
// command's working directory, and its words placed against it, the
// paths a search covers and those find, fd and xargs read under), and
// whether it touches something that cannot be placed, as the shell rule's
// "touches every protected root" has it: a working directory that cannot
// be told, a word a reading command has that cannot be placed, a line
// nested too deep or built at run time, or one too long to judge.
func probeShell(cwd, home, line string, m *msysSession) (*walker, []spot, bool) {
	ps := &setup{cwd: cwd, home: home, fold: foldCase, ctlVars: ctlVarsFixed(line), cdpath: !cdpathFixed(line) || os.Getenv("CDPATH") != "", msys: m}
	w := &walker{s: ps, cache: newSpellCache(), probe: true}
	st := cwdState{dirs: []string{cwd}}
	w.commands(line, splitCommands(line), &st, 0)
	unknown := w.reach.set() || w.tooMany
	var spots []spot
	for _, p := range w.probed {
		spots = append(spots, spot{path: p, container: true})
	}
	for _, v := range w.views {
		if !v.cwd.known() {
			unknown = true
		}
		for _, d := range v.cwd.dirs {
			spots = append(spots, spot{path: d})
		}
		_, search := searchers[v.name]
		reader := readers[v.name] || search
		// A lister's words and a cd's target name a place and read nothing
		// under it, as walker.reaches has them.
		lists := v.cwd.known() && lister(v.name, v.rest)
		mover := v.name == "cd" || v.name == "pushd" || v.name == "chdir"
		quiet := quietWords(ps, v)
		spots = append(spots, lnSpots(ps, v)...)
		if readsNameList(v) {
			// Names read from a list the gate does not see.
			spots = append(spots, spot{only: true, all: true})
		}
		// A git, hub or pbipx command that is not known to read nothing
		// under the folders it runs in or names touches them, as does the
		// directory -C moves git to, which relative words are placed against.
		switch {
		case (v.name == "git" || v.name == "hub") && !gitQuiet(v):
			spots = append(spots, gitSpots(ps, v)...)
		case v.name == "pbipx" && !pbipxQuiet(v):
			for _, d := range v.cwd.dirs {
				spots = append(spots, spot{path: d, container: true, only: true})
			}
		}
		for wi, xw := range v.words {
			abss, unk := ps.wordSpellings(v, xw)
			// A word a redirection reads from is read by whatever it is for.
			if unk && (reader || xw.redirect && xw.input && !xw.here) && !optionOnly(xw.text) {
				unknown = true
			}
			if unk && (v.name == "mv" || v.name == "ln") && !quiet[wi] && !xw.redirect && !optionOnly(xw.text) {
				// A source that cannot be told could be a folder above a
				// copy: moving or linking it escapes the registered path.
				spots = append(spots, spot{only: true, all: true})
			}
			bare := lists || mover && !xw.redirect || quiet[wi]
			for _, a := range abss {
				spots = append(spots, spot{path: a, container: true, bare: bare, prose: xw.message && !w.redefines, reads: !quiet[wi] && !xw.message})
			}
			// A path attached to a short option (-C/dir, -I/dir/x) is placed
			// for working copies only: it is a limit of the adoption finder
			// that adopted repositories keep.
			if val, ok := attachedValue(xw); ok {
				vw := xw
				vw.text, vw.pattern, vw.glob = val, val, false
				vabss, _ := ps.wordSpellings(v, vw)
				for _, a := range vabss {
					spots = append(spots, spot{path: a, container: true, bare: bare, only: true})
				}
			}
		}
		paths, everywhere := w.searchReads(v)
		if everywhere {
			unknown = true
		}
		for _, p := range paths {
			for _, d := range v.cwd.dirs {
				for _, a := range ps.spellingsAt(d, p) {
					spots = append(spots, spot{path: a, container: true})
				}
			}
		}
	}
	return w, append(spots, w.feedSpots...), unknown
}

// probeIn is reachesIn for a probe: it records the spellings of p from
// st, and an unknown working directory with a relative p as touching
// something that cannot be placed.
func (w *walker) probeIn(st cwdState, p string) {
	if !st.known() {
		if !fullPath(p) {
			w.setReach("a relative path from an unknown working directory", "")
			return
		}
		w.probed = append(w.probed, w.s.spellingsAt(string(filepath.Separator), p)...)
	} else {
		for _, d := range st.dirs {
			w.probed = append(w.probed, w.s.spellingsAt(d, p)...)
		}
	}
	if _, _, ok := w.s.msys.word(p); !ok {
		w.setReach("a path the gate cannot place", "")
	}
}

// controls decides what a call may not do from anywhere: write a file that
// controls the gate (the approval store, Claude Code's user and managed
// settings), or run 'pbipx policy approve'. An adopted repository's rules
// deny these too; outside one, they still guard every adopted repository,
// since a removed hook or a forged approval switches the gate off there.
func (f *finder) controls() Decision {
	ctl := loadControls()
	switch f.ev.Tool {
	case Write, Edit:
		if f.ev.Path == "" {
			break
		}
		paths := append(f.spellings(f.ev.Path), f.spellings(expandHome(f.ev.Path, f.home))...)
		if what, ok := ctl.target(paths); ok {
			return deny(ReasonAgentSettings, "the path is %s; %s", what, controlWhy)
		}
	case Shell:
		if f.probe == nil {
			break
		}
		for _, v := range f.probe.views {
			if what, ok := f.probe.s.reachesControl(v, ctl); ok {
				return deny(ReasonAgentSettings, "the command names a path %s; %s", what, controlWhy)
			}
			if msg, ok := personStep(v); ok {
				return deny(ReasonAgentSettings, "%s", msg)
			}
		}
	case PowerShell:
		words, ok := psWords(f.ev.Command)
		if !ok {
			return deny(ReasonInternalError, "the PowerShell command holds more than %d words, too many to judge", maxPSWords)
		}
		if d, ok := psControl(f.ev.Cwd, f.home, f.ev.Command, words, ctl); ok {
			return d
		}
	}
	return allow()
}
