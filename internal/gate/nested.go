package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
)

// A directory inside the repository that is itself a git work tree (it
// holds a .git directory or file) is a nested work tree: a linked work
// tree, such as one Claude Code adds under .claude/worktrees/, a submodule,
// or a clone. It holds a copy of the protected roots, so the protected
// roots apply relative to it as well (behavior.md, "The gate", nested work
// trees): the outer policy's, as written, and its own policy's when it has
// one that loads. A copy anywhere else (a vendored or backed-up project) is
// not recognised.

// gitEntry is the name that makes a directory a git work tree.
const gitEntry = ".git"

// nestCache remembers, for one decision, what the gate found on disk about
// nested work trees.
type nestCache struct {
	// isRoot is whether a repository-relative directory is a nested work
	// tree.
	isRoot map[string]bool
	// below are the nested work trees found under a directory.
	below map[string][]string
	// policies are a nested work tree's policies: the outer one, then its
	// own when it has one that loads.
	policies map[string][]*policy.Policy
}

func newNestCache() *nestCache {
	return &nestCache{isRoot: map[string]bool{}, below: map[string][]string{}, policies: map[string][]*policy.Policy{}}
}

// frame is a repository-relative path as one work tree sees it: the outer
// repository (prefix "") or a nested work tree at prefix, the path below
// it ("." for the work tree itself), and the policies that apply there.
type frame struct {
	prefix, sub string
	policies    []*policy.Policy
}

// roots are the protected roots of every policy of the frame.
func (f frame) roots() []string {
	var out []string
	for _, p := range f.policies {
		out = append(out, p.ProtectedRoots()...)
	}
	return out
}

// full is a path below the frame's work tree as a repository-relative
// path.
func (f frame) full(sub string) string {
	switch {
	case f.prefix == "":
		return sub
	case sub == ".":
		return f.prefix
	}
	return f.prefix + "/" + sub
}

// frames are the repository-relative path rel as the outer repository
// sees it, then as each nested work tree it is in or is sees it, outermost
// first.
func (s *setup) frames(rel string) []frame {
	out := []frame{{sub: rel, policies: []*policy.Policy{s.policy}}}
	if rel == "." || rel == "" {
		return out
	}
	segs := strings.Split(rel, "/")
	for i := 1; i <= len(segs); i++ {
		if segs[i-1] == gitEntry || segs[i-1] == ".." {
			break
		}
		dir := strings.Join(segs[:i], "/")
		if !s.isNested(dir) {
			continue
		}
		sub := "."
		if i < len(segs) {
			sub = strings.Join(segs[i:], "/")
		}
		out = append(out, frame{prefix: dir, sub: sub, policies: s.nestedPolicies(dir)})
	}
	return out
}

// isNested reports whether the repository-relative directory dir, other
// than the repository root, holds a .git entry.
func (s *setup) isNested(dir string) bool {
	if dir == "." || dir == "" {
		return false
	}
	c := s.nestCache()
	if v, ok := c.isRoot[dir]; ok {
		return v
	}
	_, err := os.Lstat(filepath.Join(s.repo.Root, filepath.FromSlash(dir), gitEntry))
	c.isRoot[dir] = err == nil
	return err == nil
}

// nestedPolicies are the policies of the nested work tree at dir: the
// outer policy, which applies relative to it, and its own when it has one
// that loads. Its own policy can only add denials: the strictest decision
// wins, and the outer policy is always applied. One that fails to load is
// left out: the outer policy still covers the work tree.
func (s *setup) nestedPolicies(dir string) []*policy.Policy {
	c := s.nestCache()
	if ps, ok := c.policies[dir]; ok {
		return ps
	}
	ps := []*policy.Policy{s.policy}
	if p, _, err := policy.LoadData(filepath.Join(s.repo.Root, filepath.FromSlash(dir), policy.FileName)); err == nil {
		ps = append(ps, p)
	}
	c.policies[dir] = ps
	return ps
}

// nestCache is the decision's cache, made on first use.
func (s *setup) nestCache() *nestCache {
	if s.nest == nil {
		s.nest = newNestCache()
	}
	return s.nest
}

// nestedBelow are the nested work trees strictly below the
// repository-relative directory rel, found by walking it on disk without
// following symlinks; nothing when rel is not a directory. A directory the
// gate cannot read is skipped: the agent runs as the same user, and
// cannot read it either.
func (s *setup) nestedBelow(rel string) []string {
	c := s.nestCache()
	if v, ok := c.below[rel]; ok {
		return v
	}
	if all, ok := c.below["."]; ok && rel != "." {
		var out []string
		for _, n := range all {
			if n != rel && hasPrefixFold(n, rel) {
				out = append(out, n)
			}
		}
		c.below[rel] = out
		return out
	}
	start := s.repo.Root
	if rel != "." {
		start = filepath.Join(s.repo.Root, filepath.FromSlash(rel))
	}
	var out []string
	_ = filepath.WalkDir(start, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if e != nil && e.IsDir() && p != start {
				return fs.SkipDir
			}
			return nil
		}
		if e.Name() != gitEntry || p == start {
			return nil
		}
		dir := filepath.Dir(p)
		if r, ok := relTo(s.repo.Root, dir, false); ok && r != "." && dir != start {
			out = append(out, r)
		}
		if e.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	slices.Sort(out)
	c.below[rel] = out
	return out
}

// nestedLocalState reports whether the repository-relative rel is, below a
// nested work tree, a local-state file or a directory local state is kept
// in under one of that work tree's policies.
func (s *setup) nestedLocalState(rel string) bool {
	for _, f := range s.frames(rel)[1:] {
		for _, p := range f.policies {
			for _, sub := range s.subSpellings(p, f.sub) {
				if class, _ := p.Classify(sub); class == policy.LocalState || p.LocalStateDir(sub) {
					return true
				}
			}
		}
	}
	return false
}

// subSpellings are sub, and on a file system that folds case sub with a
// leading protected root of p spelled as p writes it.
func (s *setup) subSpellings(p *policy.Policy, sub string) []string {
	out := []string{sub}
	if s.fold {
		if r := rootCaseIn(p.ProtectedRoots(), sub); r != sub {
			out = append(out, r)
		}
	}
	return out
}

// treeDirs are the directories a tree scan walks: the protected roots, and
// each nested work tree's roots below it.
func (s *setup) treeDirs() []string {
	dirs := s.policy.ProtectedRoots()
	for _, n := range s.nestedBelow(".") {
		for _, p := range s.nestedPolicies(n) {
			for _, root := range p.ProtectedRoots() {
				dirs = append(dirs, n+"/"+root)
			}
		}
	}
	return dirs
}

// scanFrames scans the repository-relative paths, read through r, as
// every work tree they are in sees them, under each of its policies, and
// returns every finding, with repository-relative paths.
func (s *setup) scanFrames(paths []string, r scan.Reader) finding.Result {
	type group struct {
		prefix string
		policy *policy.Policy
	}
	var order []group
	subs := map[group][]string{}
	for _, path := range paths {
		for _, f := range s.frames(path) {
			for _, p := range f.policies {
				g := group{f.prefix, p}
				if _, seen := subs[g]; !seen {
					order = append(order, g)
				}
				subs[g] = append(subs[g], s.subSpellings(p, f.sub)...)
			}
		}
	}
	var all finding.Result
	for _, g := range order {
		f := frame{prefix: g.prefix}
		opts := scan.Options{Policy: g.policy, Now: s.now}
		if g.policy == s.policy {
			opts.Baseline = s.baseline
		}
		res := scan.Run(subs[g], prefixed{r, f}, opts)
		for _, fd := range res.Findings {
			fd.Path = f.full(fd.Path)
			all.Findings = append(all.Findings, fd)
		}
	}
	finding.Sort(all.Findings)
	return all
}

// prefixed reads a path below a frame's work tree through a reader of
// repository-relative paths.
type prefixed struct {
	r scan.Reader
	f frame
}

func (p prefixed) Read(sub string) ([]byte, error) { return p.r.Read(p.f.full(sub)) }
