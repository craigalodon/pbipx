package gate

import (
	"slices"
	"strings"
)

// git's subcommands that read their options with parse-options take an
// unambiguous prefix of a long option for the option (git commit --amen
// is --amend), and --no-<name> for each option, abbreviated too. Revision
// and diff options, which log, show, diff and the like read, are not
// abbreviated. The gate expands an abbreviation to the full name, from
// these lists, before any rule looks at the options.
//
// Each list is a subcommand's long options as `git <subcommand> -h` lists
// them (git 2.53; TestGitLongOptionsMatchGit compares). A name ending in
// "=" takes a value, attached with "=" or as the next word; one ending in
// "?" takes a value only attached. Every name also has its --no- form. A
// subcommand of git stash, git notes or git config is keyed with its
// parent ("stash push").
var gitLongOptions = map[string][]string{
	"commit": {
		"ahead-behind", "all", "amend", "author=", "branch", "cleanup=", "date=", "dry-run", "edit",
		"file=", "fixup=", "gpg-sign?", "include", "only", "interactive", "long", "message=", "null",
		"patch", "pathspec-file-nul", "pathspec-from-file=", "porcelain", "quiet", "reedit-message=",
		"reset-author", "reuse-message=", "short", "signoff", "squash=", "status", "template=",
		"untracked-files?", "verbose", "allow-empty", "allow-empty-message", "inter-hunk-context=",
		"post-rewrite", "no-post-rewrite", "no-verify", "verify", "trailer=", "unified=",
	},
	"status": {
		"ahead-behind", "branch", "column?", "ignore-submodules?", "ignored?", "long", "null",
		"porcelain?", "short", "show-stash", "untracked-files?", "verbose", "find-renames?",
		"no-renames", "renames",
	},
	"stash push": {
		"all", "include-untracked", "keep-index", "message=", "patch", "pathspec-file-nul",
		"pathspec-from-file=", "quiet", "staged", "inter-hunk-context=", "unified=",
	},
	"stash save": {
		"all", "include-untracked", "keep-index", "message=", "patch", "quiet", "staged",
		"inter-hunk-context=", "unified=",
	},
	"stash apply": {"index", "quiet"},
	"stash pop":   {"index", "quiet"},
	"notes add": {
		"allow-empty", "edit", "force", "separator?", "stripspace", "file=", "message=",
		"reedit-message=", "reuse-message=",
	},
	"notes append": {
		"allow-empty", "edit", "separator?", "stripspace", "file=", "message=", "reedit-message=",
		"reuse-message=",
	},
	"merge": {
		"abort", "allow-unrelated-histories", "autostash", "cleanup=", "commit", "compact-summary",
		"continue", "edit", "ff", "gpg-sign?", "into-name=", "log?", "message=", "overwrite-ignore",
		"progress", "quiet", "quit", "rerere-autoupdate", "signoff", "squash", "stat", "strategy=",
		"strategy-option=", "summary", "verbose", "verify-signatures", "ff-only", "file=",
		"no-verify", "verify",
	},
	"add": {
		"all", "chmod=", "dry-run", "edit", "force", "ignore-errors", "ignore-missing",
		"ignore-removal", "intent-to-add", "interactive", "patch", "pathspec-file-nul",
		"pathspec-from-file=", "refresh", "renormalize", "sparse", "update", "verbose",
		"inter-hunk-context=", "no-all", "unified=",
	},
	"checkout": {
		"conflict=", "detach", "force", "guess", "ignore-other-worktrees", "ignore-skip-worktree-bits",
		"merge", "orphan=", "overlay", "overwrite-ignore", "patch", "pathspec-file-nul",
		"pathspec-from-file=", "progress", "quiet", "recurse-submodules?", "track?",
		"inter-hunk-context=", "ours", "theirs", "unified=",
	},
	"switch": {
		"conflict=", "create=", "detach", "discard-changes", "force", "force-create=", "guess",
		"ignore-other-worktrees", "merge", "orphan=", "overwrite-ignore", "progress", "quiet",
		"recurse-submodules?", "track?",
	},
	"restore": {
		"conflict=", "ignore-skip-worktree-bits", "ignore-unmerged", "merge", "overlay", "patch",
		"pathspec-file-nul", "pathspec-from-file=", "progress", "quiet", "recurse-submodules?",
		"source=", "staged", "worktree", "inter-hunk-context=", "ours", "theirs", "unified=",
	},
	"reset": {
		"intent-to-add", "patch", "pathspec-file-nul", "pathspec-from-file=", "quiet",
		"recurse-submodules?", "hard", "inter-hunk-context=", "keep", "merge", "mixed", "no-refresh",
		"refresh", "soft", "unified=",
	},
	"rebase": {
		"autosquash", "autostash", "committer-date-is-author-date", "exec=", "force-rebase",
		"fork-point", "gpg-sign?", "ignore-whitespace", "keep-base", "onto=", "quiet",
		"reapply-cherry-picks", "rebase-merges?", "rerere-autoupdate", "reschedule-failed-exec",
		"reset-author-date", "root", "signoff", "strategy=", "strategy-option=", "update-refs",
		"verbose", "whitespace=", "abort", "apply", "continue", "edit-todo", "empty=", "ff",
		"interactive", "merge", "no-ff", "no-stat", "no-verify", "quit", "show-current-patch", "skip",
		"stat", "verify",
	},
	"am": {
		"3way", "committer-date-is-author-date", "directory=", "exclude=", "gpg-sign?",
		"ignore-date", "ignore-space-change", "ignore-whitespace", "include=", "interactive", "keep",
		"keep-cr", "keep-non-patch", "message-id", "patch-format=", "quiet", "reject",
		"rerere-autoupdate", "resolvemsg=", "scissors", "signoff", "utf8", "whitespace=", "abort",
		"allow-empty", "continue", "empty=", "no-verify", "quit", "quoted-cr=", "resolved", "retry",
		"show-current-patch?", "skip", "verify",
	},
	"grep": {
		"all-match", "basic-regexp", "break", "cached", "color?", "column", "context=", "count",
		"exclude-standard", "ext-grep", "extended-regexp", "files-with-matches",
		"files-without-match", "fixed-strings", "full-name", "function-context", "heading",
		"ignore-case", "invert-match", "line-number", "max-count=", "name-only", "null",
		"only-matching", "open-files-in-pager?", "perl-regexp", "quiet", "recurse-submodules",
		"recursive", "show-function", "text", "textconv", "threads=", "untracked", "word-regexp",
		"after-context=", "and", "before-context=", "index", "max-depth=", "no-index", "not", "or",
	},
	"cat-file": {
		"buffer", "filter=", "follow-symlinks", "mailmap", "path=", "unordered", "use-mailmap",
		"batch?", "batch-all-objects", "batch-check?", "batch-command?", "filters", "textconv",
	},
	"merge-file": {
		"diff3", "marker-size=", "object-id", "ours", "quiet", "stdout", "theirs", "union", "zdiff3",
		"diff-algorithm=",
	},
	"merge-tree": {
		"merge-base=", "messages", "strategy-option=", "allow-unrelated-histories", "name-only",
		"quiet", "stdin", "trivial-merge", "write-tree",
	},
	"apply": {
		"3way", "allow-empty", "allow-overlap", "apply", "build-fake-ancestor=", "cached", "check",
		"directory=", "ignore-space-change", "ignore-whitespace", "inaccurate-eof", "index",
		"intent-to-add", "numstat", "quiet", "recount", "reject", "reverse", "stat", "summary",
		"unidiff-zero", "unsafe-paths", "verbose", "whitespace=", "add", "exclude=", "include=",
		"no-add", "ours", "theirs", "union",
	},
	"config": {
		"global", "system", "local", "worktree", "file=", "blob=", "get", "get-all",
		"get-regexp", "get-urlmatch", "replace-all", "add", "unset", "unset-all",
		"rename-section", "remove-section", "list", "fixed-value", "edit", "get-color",
		"get-colorbool", "type=", "bool", "int", "bool-or-int", "bool-or-str", "path",
		"expiry-date", "null", "name-only", "includes", "show-origin", "show-scope",
		"default=", "comment=", "all", "regexp", "value=", "url=", "show-names", "append",
	},
}

// gitOptionKey is the key of args' subcommand in gitLongOptions, and the
// index in args where its options start: after stash's, notes' or
// config's own subcommand word.
func gitOptionKey(sub string, args []string) (string, int) {
	switch sub {
	case "stash":
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			return "stash " + args[0], 1
		}
		return "stash push", 0
	case "notes":
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			return "notes " + args[0], 1
		}
		return "", 0
	}
	return sub, 0
}

// expandLong expands, in rest (sub's words after the subcommand), each
// long option before "--" that abbreviates one of sub's long options to
// the full name, keeping a value attached with "=". The value of an
// option that takes one is not read as an option. ambiguous is set when
// a word abbreviates more than one option: what git would do with it
// cannot be told from the list. rest is returned as is when nothing
// changes.
func expandLong(sub string, rest []string) (out []string, ambiguous bool) {
	key, start := gitOptionKey(sub, rest)
	opts, ok := gitLongOptions[key]
	if !ok {
		return rest, false
	}
	out = rest
	for i := start; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		full, takes, n := longOption(opts, name)
		switch {
		case n > 1:
			ambiguous = true
			continue
		case n == 0:
			continue
		}
		if full != name {
			if &out[0] == &rest[0] {
				out = slices.Clone(rest)
			}
			out[i] = "--" + full
			if hasValue {
				out[i] += "=" + value
			}
		}
		if takes && !hasValue {
			i++
		}
	}
	return out, ambiguous
}

// longOption resolves name, a long option without its dashes and value,
// against opts: the full name, whether it takes the next word as its
// value, and how many options it names (0, 1, or more when it is an
// ambiguous abbreviation). An exact name wins over abbreviations.
func longOption(opts []string, name string) (full string, takesNext bool, n int) {
	if name == "" {
		return "", false, 0
	}
	var match []string
	var takes []bool
	for _, o := range opts {
		base := strings.TrimRight(o, "=?")
		required := strings.HasSuffix(o, "=")
		for _, form := range []string{base, "no-" + base} {
			if form == name {
				return form, required && form == base, 1
			}
			if strings.HasPrefix(form, name) && !slices.Contains(match, form) {
				match = append(match, form)
				takes = append(takes, required && form == base)
			}
		}
	}
	if len(match) != 1 {
		return "", false, len(match)
	}
	return match[0], takes[0], 1
}

// gitShortValues are the short options that take a value (attached, or
// the next word when the letter ends its cluster) of the subcommands with
// a long-option list and no message (gitMessageOptions has those). A
// letter not listed there takes none.
var gitShortValues = map[string]string{
	"checkout": "bBU", "switch": "cC", "restore": "sU", "reset": "U", "add": "U",
	"grep": "efABCm", "rebase": "xsXC", "merge-file": "LX", "merge-tree": "X", "am": "CpS",
	"apply": "pC", "cat-file": "", "status": "", "config": "f",
}

// takesNext is whether an option takes the next word as its value: yes,
// no, or unknown when the gate has no list of the subcommand's options.
type takesNext int

const (
	takesNo takesNext = iota
	takesYes
	takesUnknown
)

// dashDash finds the "--" that ends sub's options in rest: the first one
// in option position, not one an option takes as its value (git commit
// -m -- -v is a message "--" and -v). k is -1 with none. For a subcommand
// whose options the gate does not list (log, diff, show ...), an option it
// cannot tell takes no value, right before a "--", leaves that "--"
// either: hidden is set when a word after it looks like an option.
func dashDash(sub string, rest []string) (k int, hidden bool) {
	key, start := gitOptionKey(sub, rest)
	for i := start; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			return i, false
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		switch optionTakesNext(sub, key, a) {
		case takesYes:
			i++
		case takesUnknown:
			if i+1 < len(rest) && rest[i+1] == "--" {
				return i + 1, slices.ContainsFunc(rest[i+2:], func(w string) bool { return len(w) > 1 && w[0] == '-' })
			}
		case takesNo:
		}
	}
	return -1, false
}

// optionTakesNext is whether option a of sub (keyed in gitLongOptions as
// key) takes the next word as its value.
func optionTakesNext(sub, key, a string) takesNext {
	opts, listed := gitLongOptions[key]
	msg, isMsg := gitMessageOptions[sub]
	if strings.HasPrefix(a, "--") {
		name, _, hasValue := strings.Cut(a[2:], "=")
		switch {
		case hasValue:
			return takesNo
		case listed:
			if _, takes, n := longOption(opts, name); n == 1 && takes {
				return takesYes
			}
			return takesNo
		case gitRevValueOpts[a]:
			return takesYes
		case slices.Contains(gitPatchFlags, a) || slices.Contains(gitSummaryFlags, a) ||
			gitStripNames[a] && !gitStripNext[a]:
			return takesNo
		}
		return takesUnknown
	}
	values, known := gitShortValues[sub]
	if isMsg {
		values, known = msg.value, true
	}
	if !known && !listed {
		switch {
		case gitRevValueOpts[a]:
			return takesYes
		case slices.Contains(gitPatchFlags, a) || slices.Contains(gitSummaryFlags, a):
			return takesNo
		}
		return takesUnknown
	}
	for j := 1; j < len(a); j++ {
		if isMsg && strings.IndexByte(msg.optional, a[j]) >= 0 {
			return takesNo
		}
		if strings.IndexByte(values, a[j]) >= 0 {
			if j == len(a)-1 {
				return takesYes
			}
			return takesNo
		}
	}
	return takesNo
}
