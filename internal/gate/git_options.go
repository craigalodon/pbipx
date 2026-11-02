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
// Each list is a subcommand's long options as git documents them. A name
// ending in "=" takes a value, attached with "=" or as the next word; one
// ending in "?" takes a value only attached. Every name also has its
// --no- form. A subcommand of git stash, git notes or git config is keyed
// with its parent ("stash push").
var gitLongOptions = map[string][]string{
	"commit": {
		"all", "patch", "interactive", "reset-author", "short", "branch", "ahead-behind",
		"porcelain", "long", "null", "reuse-message=", "reedit-message=", "fixup=", "squash=",
		"author=", "date=", "message=", "file=", "template=", "signoff", "trailer=", "verify",
		"no-verify", "allow-empty", "allow-empty-message", "cleanup=", "edit", "amend",
		"post-rewrite", "include", "only", "pathspec-from-file=", "pathspec-file-nul",
		"untracked-files?", "verbose", "quiet", "dry-run", "status", "gpg-sign?", "show-stash",
	},
	"status": {
		"short", "branch", "show-stash", "porcelain?", "long", "verbose", "null",
		"untracked-files?", "ignored?", "ignore-submodules?", "renames", "find-renames?",
		"column?", "ahead-behind",
	},
	"stash push": {
		"patch", "staged", "keep-index", "include-untracked", "all", "quiet", "message=",
		"pathspec-from-file=", "pathspec-file-nul",
	},
	"stash save": {
		"patch", "staged", "keep-index", "include-untracked", "all", "quiet", "message=",
	},
	"stash apply": {"index", "quiet"},
	"stash pop":   {"index", "quiet"},
	"notes add": {
		"message=", "file=", "reuse-message=", "reedit-message=", "force", "allow-empty",
		"separator?", "stripspace",
	},
	"notes append": {
		"message=", "file=", "reuse-message=", "reedit-message=", "allow-empty", "separator?",
		"stripspace",
	},
	"merge": {
		"commit", "edit", "cleanup=", "ff", "ff-only", "rerere-autoupdate", "verify-signatures",
		"stat", "summary", "log?", "compact-summary", "squash", "verify", "strategy=",
		"strategy-option=", "gpg-sign?", "quiet", "verbose", "progress", "autostash",
		"allow-unrelated-histories", "signoff", "into-name=", "message=", "file=", "abort",
		"quit", "continue", "overwrite-ignore",
	},
	"add": {
		"dry-run", "verbose", "interactive", "patch", "edit", "force", "update", "renormalize",
		"intent-to-add", "all", "ignore-removal", "refresh", "ignore-errors", "ignore-missing",
		"sparse", "chmod=", "pathspec-from-file=", "pathspec-file-nul", "warn-embedded-repo",
		"unified=", "inter-hunk-context=", "auto-advance",
	},
	"checkout": {
		"quiet", "progress", "force", "ours", "theirs", "track?", "guess", "detach", "orphan=",
		"ignore-skip-worktree-bits", "merge", "conflict=", "patch", "ignore-other-worktrees",
		"overwrite-ignore", "recurse-submodules?", "overlay", "pathspec-from-file=",
		"pathspec-file-nul", "unified=", "inter-hunk-context=", "auto-advance",
	},
	"switch": {
		"create=", "force-create=", "detach", "guess", "force", "discard-changes", "merge",
		"conflict=", "quiet", "progress", "track?", "orphan=", "ignore-other-worktrees",
		"recurse-submodules?", "overwrite-ignore",
	},
	"restore": {
		"source=", "patch", "worktree", "staged", "quiet", "progress", "ours", "theirs", "merge",
		"conflict=", "ignore-unmerged", "ignore-skip-worktree-bits", "recurse-submodules?",
		"overlay", "pathspec-from-file=", "pathspec-file-nul", "unified=", "inter-hunk-context=",
		"auto-advance",
	},
	"reset": {
		"quiet", "refresh", "soft", "mixed", "hard", "merge", "keep", "recurse-submodules?",
		"patch", "intent-to-add", "pathspec-from-file=", "pathspec-file-nul", "unified=",
		"inter-hunk-context=", "auto-advance",
	},
	"rebase": {
		"onto=", "keep-base", "apply", "merge", "interactive", "exec=", "root", "autosquash",
		"autostash", "fork-point", "strategy=", "strategy-option=", "rerere-autoupdate",
		"gpg-sign?", "quiet", "verbose", "stat", "verify", "signoff", "committer-date-is-author-date",
		"reset-author-date", "ignore-whitespace", "whitespace=", "ignore-date", "rebase-merges?",
		"reapply-cherry-picks", "allow-empty-message", "empty=", "keep-empty", "update-refs",
		"continue", "skip", "abort", "quit", "edit-todo", "show-current-patch", "context=",
		"reschedule-failed-exec", "trailer=",
	},
	"am": {
		"signoff", "keep", "keep-non-patch", "keep-cr", "scissors", "quoted-cr=", "message-id",
		"quiet", "utf8", "3way", "rerere-autoupdate", "ignore-space-change",
		"ignore-whitespace", "whitespace=", "directory=", "exclude=", "include=", "reject",
		"patch-format=", "interactive", "commit-date-is-author-date", "ignore-date",
		"skip", "gpg-sign?", "empty=", "continue", "resolved", "resolvemsg=", "abort", "quit",
		"show-current-patch?", "allow-empty", "verify",
	},
	"grep": {
		"cached", "untracked", "no-index", "index", "exclude-standard", "recurse-submodules",
		"invert-match", "ignore-case", "word-regexp", "text", "textconv", "max-depth=",
		"recursive", "extended-regexp", "basic-regexp", "fixed-strings", "perl-regexp",
		"line-number", "column", "full-name", "files-with-matches", "name-only",
		"files-without-match", "null", "only-matching", "count", "color?", "break", "heading",
		"show-function", "context=", "after-context=", "before-context=", "function-context",
		"max-count=", "threads=", "open-files-in-pager?", "and", "or", "not", "all-match",
		"quiet",
	},
	"cat-file": {
		"batch?", "batch-check?", "batch-command?", "batch-all-objects", "buffer", "unordered",
		"follow-symlinks", "textconv", "filters", "path=", "allow-unknown-type", "mailmap",
		"use-mailmap", "filter=",
	},
	"merge-file": {
		"stdout", "object-id", "diff3", "zdiff3", "ours", "theirs", "union", "marker-size=",
		"diff-algorithm=", "quiet",
	},
	"merge-tree": {
		"write-tree", "trivial-merge", "messages", "name-only", "allow-unrelated-histories",
		"merge-base=", "stdin", "quiet", "strategy-option=",
	},
	"apply": {
		"exclude=", "include=", "add", "stat", "numstat", "summary", "check", "index",
		"intent-to-add", "cached", "apply", "3way", "build-fake-ancestor=", "whitespace=",
		"ignore-space-change", "ignore-whitespace", "reverse", "unidiff-zero", "reject",
		"allow-overlap", "verbose", "quiet", "inaccurate-eof", "recount", "directory=",
		"allow-empty",
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
