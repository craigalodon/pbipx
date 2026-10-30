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
type spot struct {
	path      string
	container bool
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
}

// decideAdopted decides ev under opts.Adoption: against each adopted
// repository it touches (targets), any deny winning, and then the files
// outside every repository that control the gate (controls). A call that
// touches no adopted repository and no such file is allowed.
func decideAdopted(ev Event, opts Options) Decision {
	if ev.Cwd == "" || !filepath.IsAbs(ev.Cwd) {
		return deny(ReasonEventError, "the event has no absolute working directory")
	}
	f := &finder{ev: ev, store: opts.Adoption.store(), home: userHome(), fold: foldCase,
		gitAt: map[string]bool{}, adopted: map[string]bool{}}
	if f.store != nil {
		roots, records, err := f.store.Roots()
		if err != nil {
			return deny(ReasonInternalError, "the index of repositories set up for pbipx cannot be read; the call is denied")
		}
		f.records = records
		for _, r := range roots {
			f.index = append(f.index, r, resolve(r))
		}
		slices.Sort(f.index)
		f.index = slices.Compact(f.index)
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

func (f *finder) adoptedNow(root string) bool {
	if _, err := os.Lstat(filepath.Join(root, policy.FileName)); err == nil {
		return true
	}
	for _, a := range []string{root, resolve(root)} {
		if slices.ContainsFunc(f.index, func(r string) bool { return samePath(r, a, f.fold) }) {
			return true
		}
	}
	if !f.records {
		return false
	}
	id, err := Identify(root)
	if err != nil {
		// git cannot name the clone, so no record can be found for it.
		return false
	}
	ok, err := f.store.Adopted(id)
	return ok || err != nil
}

// spots are the paths ev touches, and whether it touches something that
// cannot be placed: the working directory; a read's or a write's file; a
// search's path; and for a shell call, every working directory a command
// runs in and every word placed against it (probeShell).
func (f *finder) spots() (spots []spot, unknown bool) {
	ev := f.ev
	addAll := func(paths []string, container bool) {
		for _, p := range paths {
			spots = append(spots, spot{path: p, container: container})
		}
	}
	addAll(spellings(ev.Cwd, "."), false)
	switch ev.Tool {
	case Read, Write:
		if ev.Path != "" {
			addAll(spellings(ev.Cwd, expandHome(ev.Path, f.home)), false)
		}
	case Search:
		p := ev.Path
		if p == "" {
			p = ev.Cwd
		}
		addAll(spellings(ev.Cwd, expandHome(p, f.home)), true)
	case Shell:
		var probed []spot
		f.probe, probed, unknown = probeShell(ev.Cwd, f.home, ev.Command)
		spots = append(spots, probed...)
	}
	return spots, unknown
}

// probeShell walks a shell line as the shell rule does, with no repository,
// judging nothing: the walker that read it, the paths it touches (each
// command's working directory, and its words placed against it, the
// paths a search covers and those find, fd and xargs read under), and
// whether it touches something that cannot be placed, as the shell rule's
// "touches every protected root" has it: a working directory that cannot
// be told, a word a reading command has that cannot be placed, a line
// nested too deep or built at run time, or one too long to judge.
func probeShell(cwd, home, line string) (*walker, []spot, bool) {
	ps := &setup{cwd: cwd, home: home, fold: foldCase}
	w := &walker{s: ps, cache: newSpellCache(), probe: true}
	st := cwdState{dirs: []string{cwd}}
	w.commands(line, splitCommands(line), &st, 0)
	unknown := w.reach || w.tooMany
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
		reader := readers[v.name] || searchers[v.name].always || searchers[v.name].recursive != ""
		for _, xw := range v.words {
			abss, unk := ps.wordSpellings(v, xw)
			if unk && reader && !optionOnly(xw.text) {
				unknown = true
			}
			for _, a := range abss {
				spots = append(spots, spot{path: a, container: true})
			}
		}
		if paths, ok := searchPaths(v.argv); ok {
			for _, p := range paths {
				for _, d := range v.cwd.dirs {
					for _, a := range spellings(d, p) {
						spots = append(spots, spot{path: a, container: true})
					}
				}
			}
		}
	}
	return w, spots, unknown
}

// probeIn is reachesIn for a probe: it records the spellings of p from
// st, and an unknown working directory with a relative p as touching
// something that cannot be placed.
func (w *walker) probeIn(st cwdState, p string) {
	if !st.known() {
		if !fullPath(p) {
			w.reach = true
			return
		}
		w.probed = append(w.probed, spellings(string(filepath.Separator), p)...)
		return
	}
	for _, d := range st.dirs {
		w.probed = append(w.probed, spellings(d, p)...)
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
	case Write:
		if f.ev.Path == "" {
			break
		}
		paths := append(spellings(f.ev.Cwd, f.ev.Path), spellings(f.ev.Cwd, expandHome(f.ev.Path, f.home))...)
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
			if selfApproval(v) {
				return deny(ReasonAgentSettings, "the command runs 'pbipx policy approve'; "+
					"approving the policy is a person's step at a terminal, and an agent may not approve the policy that binds it")
			}
		}
	}
	return allow()
}
