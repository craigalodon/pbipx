package gate

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Git subcommands that always print file content (behavior.md, "The gate",
// Bash rule 2).
var gitAlwaysContent = map[string]bool{
	"blame": true, "annotate": true, "grep": true, "archive": true,
	"format-patch": true, "difftool": true, "range-diff": true,
}

// Git subcommands that print content, or run a command, whatever their
// pathspecs say: what they print cannot be placed.
var gitUnplaceable = map[string]bool{
	"fast-export": true, "pack-objects": true, "unpack-file": true, "filter-branch": true,
}

// gitSummaryFlags turn diff and show into a summary with no content.
var gitSummaryFlags = []string{
	"--stat", "--numstat", "--shortstat", "--name-only", "--name-status",
	"--dirstat", "--compact-summary", "--summary", "--raw", "--no-patch",
	"-s", "--quiet",
}

// gitPatchFlags make git print a patch, or search content (-S, -G): content.
// Each also matches with a value attached ("-U3", "--unified=3",
// "-L1,2:file").
var gitPatchFlags = []string{
	"-p", "-u", "--patch", "--patch-with-stat", "--patch-with-raw", "-U", "--unified",
	"--cc", "-c", "--dd", "--remerge-diff", "--full-diff", "-L", "--word-diff", "--color-words",
	"--function-context", "-W", "--binary", "--ext-diff", "--textconv", "-S", "-G",
}

// gitValueFlags are flags whose value can be attached without "=".
var gitValueFlags = []string{"-U", "-L", "-S", "-G"}

// gitCatFileSafe are the cat-file modes that print no content.
var gitCatFileSafe = []string{"-t", "-s", "-e", "--batch-check"}

// gitRefSets are revision options of show that name objects by the
// repository's refs, or read them from standard input: what they show
// cannot be told from the text.
var gitRefSets = []string{
	"--all", "--branches", "--tags", "--remotes", "--glob", "--stdin", "--reflog",
	"--bisect", "--alternate-refs", "--indexed-objects",
}

// gitGlobalWithValue are git's global options that take the next word.
var gitGlobalWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--config-env": true, "--super-prefix": true, "--list-cmds": true, "--attr-source": true,
}

// gitBuiltins are git's own commands. git runs a built-in command before it
// looks for an alias of the same name, so these are never resolved as
// aliases; any other name may be one. Every command this file judges is
// here.
var gitBuiltins = map[string]bool{}

func init() {
	for _, list := range [][]string{
		mapKeys(gitAlwaysContent), mapKeys(gitUnplaceable),
		{
			"add", "am", "apply", "bisect", "branch", "bundle", "cat-file", "check-attr",
			"check-ignore", "check-mailmap", "check-ref-format", "checkout", "checkout-index",
			"cherry", "cherry-pick", "clean", "clone", "column", "commit", "commit-graph",
			"commit-tree", "config", "count-objects", "describe", "diff", "diff-files",
			"diff-index", "diff-tree", "fetch", "fetch-pack", "for-each-ref", "for-each-repo",
			"fsck", "gc", "hash-object", "help", "index-pack", "init", "interpret-trailers",
			"log", "ls-files", "ls-remote", "ls-tree", "maintenance", "merge", "merge-base",
			"merge-file", "merge-tree", "mktag", "mktree", "mv", "name-rev", "notes", "prune",
			"pull", "push", "read-tree", "rebase", "reflog", "remote", "repack", "replace",
			"replay", "request-pull", "rerere", "reset", "restore", "rev-list", "rev-parse",
			"revert", "rm", "shortlog", "show", "show-branch", "show-index", "show-ref",
			"sparse-checkout", "stash", "status", "submodule", "switch", "symbolic-ref", "tag",
			"update-index", "update-ref", "var", "verify-commit", "verify-pack", "verify-tag",
			"version", "whatchanged", "worktree", "write-tree", "mergetool", "instaweb",
		},
	} {
		for _, name := range list {
			gitBuiltins[name] = true
		}
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// gitInvocation is one git command line, read far enough to judge it.
type gitInvocation struct {
	sub string // the subcommand
	// rest is every word after the subcommand, and args those before any
	// "--".
	rest, args []string
	// specs are the words after "--", and dashdash whether there was one.
	specs    []string
	dashdash bool
	// dir is the directory relative paths start from (-C moves it).
	dir string
	// elsewhere is true when --git-dir or --work-tree point git at a tree
	// these paths cannot be checked against.
	elsewhere bool
	// unsafe is true when a global option can make git run a command, or
	// print what these rules cannot see: -c or --config-env with a key
	// not known to be harmless, --exec-path=.
	unsafe bool
	// oddSpecs is true when a global option changes how every pathspec
	// matches (--literal-pathspecs, --glob-pathspecs, --icase-pathspecs).
	oddSpecs bool
}

// parseGit reads a git command's words after "git". ok is false when there
// is no subcommand.
func parseGit(words []string, cwd string) (inv gitInvocation, ok bool) {
	inv.dir = cwd
	return inv.parse(words)
}

// parse reads words, global options then a subcommand, on top of what inv
// already holds.
func (inv gitInvocation) parse(words []string) (gitInvocation, bool) {
	i := 0
	for i < len(words) {
		w := words[i]
		name, value, hasValue := strings.Cut(w, "=")
		if !strings.HasPrefix(w, "-") {
			break
		}
		next := ""
		if i+1 < len(words) {
			next = words[i+1]
		}
		switch {
		case w == "-C":
			if next != "" {
				inv.dir = abs(inv.dir, next)
			}
		case name == "--git-dir" || name == "--work-tree":
			inv.elsewhere = true
		case w == "-c":
			inv.unsafe = inv.unsafe || !safeConfig(next)
		case name == "--config-env":
			kv := next
			if hasValue {
				kv = value
			}
			// The value comes from the environment: only its key is known.
			key, _, _ := strings.Cut(kv, "=")
			inv.unsafe = inv.unsafe || !safeConfig(key+"=\x00")
		case name == "--exec-path" && hasValue:
			inv.unsafe = true
		case w == "--literal-pathspecs" || w == "--glob-pathspecs" || w == "--icase-pathspecs":
			inv.oddSpecs = true
		}
		i++
		if gitGlobalWithValue[name] && !hasValue {
			i++
		}
	}
	if i >= len(words) {
		return inv, false
	}
	inv.sub = words[i]
	inv.rest = words[i+1:]
	inv.args, inv.specs, inv.dashdash = inv.rest, nil, false
	if k := slices.Index(inv.rest, "--"); k >= 0 {
		inv.args, inv.specs, inv.dashdash = inv.rest[:k], inv.rest[k+1:], true
	}
	return inv, true
}

// safeConfig reports whether a -c key=value setting is known to change
// nothing these rules depend on: it cannot run a command (an alias, a
// pager, an editor, a diff or textconv driver, a filter, a hook), and does
// not change what a command prints (stash.showPatch, log.*). Every other
// key is unprovable. A value of "\x00" stands for one that is not known.
func safeConfig(kv string) bool {
	key, value, _ := strings.Cut(kv, "=")
	key = strings.ToLower(key)
	section, _, _ := strings.Cut(key, ".")
	switch section {
	case "color", "column", "advice", "user", "author", "committer", "i18n", "init", "safe":
		return true
	case "pager":
		return safePager(value) || isBool(value)
	}
	switch key {
	case "core.pager":
		return safePager(value)
	case "core.editor", "sequence.editor":
		return value == "true" || value == ":"
	case "core.quotepath", "core.autocrlf", "core.safecrlf", "core.eol", "core.abbrev",
		"core.filemode", "commit.gpgsign", "tag.gpgsign", "gc.auto", "maintenance.auto",
		"fetch.prune", "pull.rebase", "pull.ff", "push.default", "push.autosetupremote",
		"rebase.autostash", "status.short", "status.branch", "status.showuntrackedfiles",
		"diff.noprefix", "diff.renames", "diff.mnemonicprefix", "merge.ff", "merge.conflictstyle":
		return true
	}
	return false
}

// safePager reports whether a pager setting only passes git's output on.
func safePager(v string) bool { return v == "" || v == "cat" }

func isBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "1", "0":
		return true
	}
	return false
}

// unsafeEnv reports whether environment assignments before git can make it
// run a command or read what these rules cannot see: GIT_CONFIG_*, GIT_DIR,
// GIT_EXTERNAL_DIFF, GIT_PAGER and the like. A pager set to cat and an
// editor set to true are harmless, and so are identities and tracing.
func unsafeEnv(words []string) bool {
	for _, w := range words {
		if !isAssignment(w) {
			continue
		}
		name, value, _ := strings.Cut(w, "=")
		switch {
		case name == "GIT_PAGER" || name == "PAGER":
			if !safePager(value) {
				return true
			}
		case name == "GIT_EDITOR" || name == "GIT_SEQUENCE_EDITOR" || name == "EDITOR" || name == "VISUAL":
			if value != "true" && value != ":" {
				return true
			}
		case name == "LESSOPEN" || name == "LESSCLOSE":
			return true
		case strings.HasPrefix(name, "GIT_AUTHOR_"), strings.HasPrefix(name, "GIT_COMMITTER_"),
			strings.HasPrefix(name, "GIT_TRACE"), name == "GIT_TERMINAL_PROMPT",
			name == "GIT_OPTIONAL_LOCKS", name == "GIT_FLUSH", name == "GIT_PROGRESS_DELAY",
			name == "GIT_ADVICE":
		case strings.HasPrefix(name, "GIT_"):
			return true
		}
	}
	return false
}

// hasFlag reports whether args hold one of flags, alone, with "=value", or,
// for those in gitValueFlags, with the value attached.
func hasFlag(args, flags []string) bool {
	for _, a := range args {
		for _, f := range flags {
			switch {
			case a == f, strings.HasPrefix(a, f+"="):
				return true
			case slices.Contains(gitValueFlags, f) && strings.HasPrefix(a, f) && len(a) > len(f):
				return true
			}
		}
	}
	return false
}

// hasShort reports whether args hold the long option long, or the short
// option c alone or in a cluster ("-ap"), where a cluster ends at the
// first letter of withValue, whose value follows it.
func hasShort(args []string, c byte, long, withValue string) bool {
	for _, a := range args {
		if a == long || (long != "" && strings.HasPrefix(a, long+"=")) {
			return true
		}
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for j := 1; j < len(a); j++ {
			if a[j] == c {
				return true
			}
			if strings.IndexByte(withValue, a[j]) >= 0 {
				break
			}
		}
	}
	return false
}

// positional is the arguments that are not options.
func positional(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// pathspec is one path a git command is limited to, or excludes.
type pathspec struct {
	rel     string // repository-relative, clean; "" when it cannot be placed
	exclude bool
	// unprovable is true when what it matches cannot be told from the
	// text: a glob, case folding, attributes, or a path outside the
	// repository's tree.
	unprovable bool
}

// unprovable is the answer for content whose source cannot be placed.
func unprovable() []pathspec { return []pathspec{{unprovable: true}} }

// gitContent decides rule 2 for one simple command: whether it is git
// printing content that could come from a protected root. args is the
// whole simple command, with any assignments and wrappers before git. It
// returns the subcommand when the command is denied, and "" otherwise.
func (s *setup) gitContent(args []string) string {
	name, rest := program(args)
	if name != "git" {
		return ""
	}
	inv, ok := parseGit(rest, s.cwd)
	if !ok {
		return ""
	}
	if unsafeEnv(args[:len(args)-len(rest)-1]) {
		return inv.sub
	}
	inv, ok = s.resolveAlias(inv)
	if !ok || inv.unsafe {
		return inv.sub
	}
	specs, content := s.gitSpecs(inv)
	if !content {
		return ""
	}
	if !inv.elsewhere && !inv.oddSpecs && s.avoidsRoots(specs) {
		return ""
	}
	return inv.sub
}

// resolveAlias returns the invocation git runs when inv's subcommand is an
// alias: its expansion, split as git splits it, then inv's arguments. ok is
// false when that cannot be told: a shell alias ("!"), an alias of an
// alias, an expansion that does not split, or git failing to answer.
func (s *setup) resolveAlias(inv gitInvocation) (gitInvocation, bool) {
	if gitBuiltins[inv.sub] || inv.unsafe {
		return inv, true
	}
	if inv.elsewhere {
		// The alias would come from another repository's configuration.
		return inv, false
	}
	expansion, err := configValue(inv.dir, "alias."+inv.sub, "")
	if errors.Is(err, errNotSet) {
		return inv, true // not an alias: another program, or a typo
	}
	if err != nil || strings.HasPrefix(expansion, "!") {
		return inv, false
	}
	words, ok := splitAlias(expansion)
	if !ok || len(words) == 0 {
		return inv, false
	}
	next, ok := inv.parse(slices.Concat(words, inv.rest))
	if !ok {
		return inv, false
	}
	if !gitBuiltins[next.sub] {
		if _, err := configValue(next.dir, "alias."+next.sub, ""); !errors.Is(err, errNotSet) {
			return inv, false
		}
	}
	return next, true
}

// splitAlias splits an alias's expansion into words as git does
// (split_cmdline): at whitespace, with single and double quotes and
// backslash escapes. ok is false on an unclosed quote or a trailing
// backslash.
func splitAlias(v string) ([]string, bool) {
	var (
		out     []string
		buf     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	for _, r := range v {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped, inWord = false, true
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				out = append(out, buf.String())
				buf.Reset()
				inWord = false
			}
		default:
			buf.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inWord {
		out = append(out, buf.String())
	}
	return out, true
}

// gitSpecs says whether inv prints content and, if it does, the paths that
// content can come from. Nil specs with content true is the whole tree.
func (s *setup) gitSpecs(inv gitInvocation) (specs []pathspec, content bool) {
	patch := hasFlag(inv.args, gitPatchFlags)
	after := func() []pathspec {
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return unprovable()
		}
		return s.parseSpecs(inv.specs, inv.dir)
	}
	switch inv.sub {
	case "blame", "annotate":
		return s.blameSpecs(inv), true
	case "diff":
		if hasFlag(inv.args, gitSummaryFlags) && !patch {
			return nil, false
		}
		return after(), true
	case "diff-tree":
		if !patch && !hasFlag(inv.args, []string{"--stdin"}) {
			return nil, false
		}
		return after(), true
	case "diff-index", "diff-files":
		if !patch {
			return nil, false
		}
		return after(), true
	case "show":
		return s.showSpecs(inv, patch)
	case "log", "reflog", "whatchanged":
		if !patch {
			return nil, false
		}
		return after(), true
	case "stash":
		return s.stashSpecs(inv)
	case "add":
		if !hasShort(inv.args, 'p', "--patch", "") && !hasShort(inv.args, 'i', "--interactive", "") {
			return nil, false
		}
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return unprovable(), true
		}
		return s.parseSpecs(slices.Concat(positional(inv.args), inv.specs), inv.dir), true
	case "restore":
		if !hasShort(inv.args, 'p', "--patch", "s") {
			return nil, false
		}
		if hasFlag(inv.args, []string{"--pathspec-from-file"}) {
			return unprovable(), true
		}
		return s.parseSpecs(slices.Concat(positional(inv.args), inv.specs), inv.dir), true
	case "checkout", "reset":
		if !hasShort(inv.args, 'p', "--patch", "bB") {
			return nil, false
		}
		return after(), true
	case "commit":
		if !hasShort(inv.args, 'p', "--patch", "mFCcSt") && !hasFlag(inv.args, []string{"--interactive"}) {
			return nil, false
		}
		return after(), true
	case "rebase", "am":
		if hasFlag(inv.args, []string{"--show-current-patch"}) ||
			(inv.sub == "rebase" && hasShort(inv.args, 'x', "--exec", "sSXo")) {
			return unprovable(), true
		}
		return nil, false
	case "request-pull":
		return unprovable(), hasShort(inv.args, 'p', "", "")
	case "bundle":
		return unprovable(), bundleToOutput(inv.args)
	case "merge-tree":
		// The trivial merge prints the merged content; --write-tree, the
		// default for two commits, prints object names.
		trivial := hasFlag(inv.args, []string{"--trivial-merge"}) ||
			(!hasFlag(inv.args, []string{"--write-tree"}) && len(positional(inv.args)) >= 3)
		return unprovable(), trivial
	case "merge-file":
		return unprovable(), hasFlag(inv.args, []string{"--object-id"})
	case "submodule":
		return unprovable(), slices.Contains(positional(inv.args), "foreach")
	case "bisect":
		return unprovable(), len(inv.args) > 0 && inv.args[0] == "run"
	case "cat-file":
		return s.catFileSpecs(inv)
	}
	if gitUnplaceable[inv.sub] {
		return unprovable(), true
	}
	if gitAlwaysContent[inv.sub] {
		return after(), true
	}
	return nil, false
}

// blameSpecs are the paths blame and annotate can read: every positional
// argument, before "--" or after it, and the value of --contents. git takes
// the file before or after the revision, so each one may be it.
func (s *setup) blameSpecs(inv gitInvocation) []pathspec {
	var paths []string
	for i := 0; i < len(inv.args); i++ {
		switch a := inv.args[i]; {
		case a == "-L":
			i++ // a line range, not a file
		case !strings.HasPrefix(a, "-"):
			// A file, a revision, or the value of --contents, -S or
			// --ignore-revs-file, which are files too.
			paths = append(paths, a)
		case strings.HasPrefix(a, "--contents="), strings.HasPrefix(a, "--ignore-revs-file="):
			_, v, _ := strings.Cut(a, "=")
			paths = append(paths, v)
		}
	}
	paths = append(paths, inv.specs...)
	if len(paths) == 0 {
		return nil
	}
	return s.literalSpecs(paths, inv.dir)
}

// showSpecs judges git show. Each object it is given is looked up in the
// repository: a commit prints a diff (unless a summary flag is given), a
// blob or a tree prints itself, a tag prints its target, and an object
// that cannot be resolved, or is named by searching commit messages, cannot
// be placed.
func (s *setup) showSpecs(inv gitInvocation, patch bool) ([]pathspec, bool) {
	if hasFlag(inv.args, gitRefSets) {
		return unprovable(), true
	}
	diff := !hasFlag(inv.args, gitSummaryFlags) || patch
	var blobs []string
	commit := false
	revs := showRevs(inv.args)
	for _, a := range revs {
		switch kind, p := s.kindOf(inv, a); kind {
		case revBlobPath:
			blobs = append(blobs, p)
		case revCommit:
			commit = true
		default:
			return unprovable(), true
		}
	}
	if len(revs) == 0 {
		commit = true // HEAD
	}
	specs := s.blobSpecs(blobs, inv.dir)
	switch {
	case diff && commit && len(inv.specs) == 0:
		return nil, true
	case diff && commit:
		specs = append(specs, s.parseSpecs(inv.specs, inv.dir)...)
	}
	return specs, len(specs) > 0
}

// showRevs is show's positional arguments: its objects. The value of -n is
// skipped.
func showRevs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-n":
			i++
		case !strings.HasPrefix(a, "-"):
			out = append(out, a)
		}
	}
	return out
}

// revKind is what a revision argument names.
type revKind int

const (
	revUnplaceable revKind = iota
	revCommit              // a commit, a range of commits, or a tag of one
	revBlobPath            // REV:path or :path, with the path returned
)

// kindOf is what a revision argument of show names.
func (s *setup) kindOf(inv gitInvocation, a string) (revKind, string) {
	if strings.Contains(a, ":/") || strings.Contains(a, "^{/") {
		return revUnplaceable, "" // a search of commit messages
	}
	if p, ok := revPath(a); ok {
		return revBlobPath, p
	}
	if inv.elsewhere {
		return revUnplaceable, ""
	}
	if left, right, ok := cutRange(a); ok {
		for _, side := range []string{left, right} {
			if side != "" && objectType(inv.dir, side+"^{commit}") != "commit" {
				return revUnplaceable, ""
			}
		}
		return revCommit, ""
	}
	if strings.HasPrefix(a, "^") {
		if objectType(inv.dir, a[1:]+"^{commit}") == "commit" {
			return revCommit, ""
		}
		return revUnplaceable, ""
	}
	switch objectType(inv.dir, a) {
	case "commit":
		return revCommit, ""
	case "tag":
		// An annotated tag prints its target: a commit's diff is judged as
		// a commit's, anything else is content.
		if objectType(inv.dir, a+"^{}") == "commit" {
			return revCommit, ""
		}
	}
	return revUnplaceable, ""
}

// cutRange splits A..B and A...B.
func cutRange(a string) (left, right string, ok bool) {
	if l, r, found := strings.Cut(a, "..."); found {
		return l, r, true
	}
	return strings.Cut(a, "..")
}

// stashSpecs judges git stash. stash show prints a diff with a patch flag,
// or with none at all when stash.showPatch is true; stash -p and stash push
// -p print hunks.
func (s *setup) stashSpecs(inv gitInvocation) ([]pathspec, bool) {
	sub, args := "push", inv.args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "show":
		if hasFlag(args, gitPatchFlags) {
			return s.parseSpecs(inv.specs, inv.dir), true
		}
		if hasFlag(args, gitSummaryFlags) {
			return nil, false
		}
		v, err := configValue(inv.dir, "stash.showPatch", "bool")
		if errors.Is(err, errNotSet) || (err == nil && v == "false") {
			return nil, false
		}
		return s.parseSpecs(inv.specs, inv.dir), true
	case "push", "save":
		if !hasShort(args, 'p', "--patch", "m") {
			return nil, false
		}
		return s.parseSpecs(inv.specs, inv.dir), true
	}
	return nil, false
}

// bundleToOutput reports whether args, after "bundle", create a bundle on
// standard output ("-") or a device, where its objects are printed.
func bundleToOutput(args []string) bool {
	if len(args) == 0 || args[0] != "create" {
		return false
	}
	for _, a := range args[1:] {
		if a == "-" || !strings.HasPrefix(a, "-") {
			return a == "-" || strings.HasPrefix(a, "/dev/")
		}
	}
	return false
}

// catFileSpecs judges git cat-file: every mode but -t, -s, -e and
// --batch-check prints an object.
func (s *setup) catFileSpecs(inv gitInvocation) ([]pathspec, bool) {
	if hasFlag(inv.args, gitCatFileSafe) {
		return nil, false
	}
	var blobs []string
	for _, a := range positional(inv.args) {
		switch a {
		case "blob", "tree", "commit", "tag":
			continue
		}
		if strings.Contains(a, ":/") || strings.Contains(a, "^{/") {
			return unprovable(), true
		}
		p, ok := revPath(a)
		if !ok {
			// An object name: what it holds cannot be told.
			return unprovable(), true
		}
		blobs = append(blobs, p)
	}
	if len(blobs) == 0 {
		// --batch reads object names from standard input.
		return unprovable(), true
	}
	return s.blobSpecs(blobs, inv.dir), true
}

// revPath splits REV:path, as show and cat-file take it, and returns the
// path. ":path" and ":N:path" name the index.
func revPath(a string) (string, bool) {
	if strings.HasPrefix(a, ":") {
		rest := a[1:]
		if len(rest) >= 2 && isDigit(rune(rest[0])) && rest[1] == ':' {
			rest = rest[2:]
		}
		return rest, true
	}
	_, p, ok := strings.Cut(a, ":")
	return p, ok
}

// blobSpecs places REV:path paths: relative to the repository root, unless
// they start with ./ or ../, which are relative to dir.
func (s *setup) blobSpecs(paths []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		if p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
			out = append(out, s.place(abs(dir, p), false))
			continue
		}
		out = append(out, s.place(filepath.Join(s.repo.Root, filepath.FromSlash(p)), false))
	}
	return out
}

// literalSpecs places file paths that git takes literally, relative to dir.
func (s *setup) literalSpecs(paths []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(paths))
	for _, p := range paths {
		out = append(out, s.place(abs(dir, p), false))
	}
	return out
}

// parseSpecs reads pathspecs with their magic (gitglossary, "pathspec"):
// ":!x", ":^x" and ":(exclude)x" exclude; ":/x" and ":(top)x" start from
// the repository root; ":(literal)" turns globs off. Other magic, and a
// glob, make a pathspec unprovable.
func (s *setup) parseSpecs(specs []string, dir string) []pathspec {
	out := make([]pathspec, 0, len(specs))
	for _, raw := range specs {
		exclude, top, literal, odd := false, false, false, false
		p := raw
		switch {
		case strings.HasPrefix(p, ":("):
			end := strings.Index(p, ")")
			if end < 0 {
				out = append(out, pathspec{unprovable: true})
				continue
			}
			for m := range strings.SplitSeq(p[2:end], ",") {
				switch m {
				case "exclude":
					exclude = true
				case "top":
					top = true
				case "literal":
					literal = true
				default: // glob, icase, attr:...
					odd = true
				}
			}
			p = p[end+1:]
		case strings.HasPrefix(p, ":"):
			i := 1
			for i < len(p) && strings.ContainsRune("/!^", rune(p[i])) {
				switch p[i] {
				case '/':
					top = true
				default:
					exclude = true
				}
				i++
			}
			if i < len(p) && p[i] == ':' {
				i++
			}
			p = p[i:]
		}
		if odd || (!literal && hasGlob(p)) {
			out = append(out, pathspec{exclude: exclude, unprovable: true})
			continue
		}
		a := abs(dir, p)
		if top {
			a = filepath.Join(s.repo.Root, filepath.FromSlash(p))
		}
		out = append(out, s.place(a, exclude))
	}
	return out
}

// place makes a pathspec of the absolute path a.
func (s *setup) place(a string, exclude bool) pathspec {
	r, ok := s.rel(a)
	if !ok {
		if r2, ok2 := s.rel(resolve(a)); ok2 {
			r, ok = r2, true
		}
	}
	if !ok {
		return pathspec{exclude: exclude, unprovable: true}
	}
	return pathspec{rel: path.Clean(r), exclude: exclude}
}

// avoidsRoots reports whether pathspecs provably keep a git command away
// from every protected root: each root is excluded, or no positive pathspec
// touches it. No positive pathspec means the whole tree. An exclusion
// counts only when it can be placed and covers the whole root.
func (s *setup) avoidsRoots(specs []pathspec) bool {
	var positives []pathspec
	for _, sp := range specs {
		if !sp.exclude {
			positives = append(positives, sp)
		}
	}
	if len(positives) == 0 {
		positives = []pathspec{{rel: "."}}
	}
	for _, root := range s.policy.ProtectedRoots() {
		excluded := false
		for _, sp := range specs {
			if sp.exclude && !sp.unprovable && (sp.rel == "." || root == sp.rel || strings.HasPrefix(root, sp.rel+"/")) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		for _, sp := range positives {
			if sp.unprovable || touchesRoot(sp.rel, root) {
				return false
			}
		}
	}
	return true
}

// touchesRoot reports whether the repository-relative path rel is root, is
// inside it, or contains it.
func touchesRoot(rel, root string) bool {
	return rel == "." || rel == root || strings.HasPrefix(rel, root+"/") || strings.HasPrefix(root, rel+"/")
}
