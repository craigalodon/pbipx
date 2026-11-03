package gate

import (
	"regexp"
	"slices"
	"strings"
)

// positionalText matches a positional parameter or OPTARG in the text of
// an expansion or an arithmetic command: $1, ${1}, $@, $*, $#, ${@:2}, a
// bare OPTARG.
var positionalText = regexp.MustCompile(`\$\{?[0-9@*#]|OPTARG`)

// positionalRef reports whether c reads a positional parameter or OPTARG,
// in a word, an expansion or arithmetic. The arguments of set, of a
// function and of getopts are then values arithmetic can read.
func positionalRef(c simple) bool {
	if positionalText.MatchString(c.arithText) {
		return true
	}
	for _, wd := range c.words {
		for _, p := range wd.parts {
			switch {
			case p.kind == paramPart && (p.text == "OPTARG" || p.text != "" && strings.IndexByte("0123456789@*#", p.text[0]) >= 0):
				return true
			case p.kind != litPart && positionalText.MatchString(p.raw):
				return true
			}
		}
	}
	return false
}

// Runners that run a command on words they read from their input: xargs,
// and GNU parallel. The words are file names or anything else, so for the
// command they run they are words the gate cannot tell: the replacement
// string of xargs (-I R, -i, --replace), parallel's placeholders ({},
// {.}, {/}, {//}, {/.}, {#}, {%}, {n}), and, with no placeholder, the
// words appended to the command. find -exec's {} is not one: find's own
// walk names the files, which the path find searches covers.

// runnerFeed says how a runner in a command's words feeds the command it
// runs. The zero value is no runner.
type runnerFeed struct {
	// runner is "xargs" or "parallel", "" when the command is under
	// neither.
	runner string
	// fromInput is true when the words come from standard input or a file
	// the gate does not read (parallel with no ::: source of its own).
	fromInput bool
	// placeholders are the texts in the command that stand for an input
	// word, wherever they appear in what the runner runs.
	placeholders []string
	// appends is true when the input words are added after the command:
	// from input, with no placeholder.
	appends bool
}

// parallelPlaceholders are the texts GNU parallel replaces with an input
// word: {} and its variants, {#} and {%} (job and slot numbers, not
// names), the positional {1}...{9} with the same variants, and {= perl
// expression =}.
var parallelPlaceholders = func() []string {
	out := []string{"{}", "{.}", "{/}", "{//}", "{/.}", "{#}", "{%}", "{="}
	for _, d := range "123456789" {
		n := string(d)
		out = append(out, "{"+n+"}", "{"+n+".}", "{"+n+"/}", "{"+n+"//}", "{"+n+"/.}")
	}
	return out
}()

// fedAll stands in a runner's placeholders for "every word": the gate
// could not read the runner's options completely, so any word of the
// command it runs, nested lines included, may be an input word.
const fedAll = "\x00fed-all"

// feedOf reads argv, a command's words (known says which the gate can
// tell), for a runner (xargs or parallel, the first of them) and how it
// feeds what it runs. A runner whose options it cannot read completely —
// an option it does not know, a cluster it cannot split, a value it
// cannot tell or that is no number where one is due, a word it cannot
// expand among them or as the command — feeds every word (fedAll).
func feedOf(argv []string, known []bool) runnerFeed {
	// Only a wrapper of the command or its program is a runner: an
	// argument that happens to be named xargs or parallel is a word.
	name, rest, under := command(argv)
	if !slices.Contains(under, "xargs") && !slices.Contains(under, "parallel") &&
		name != "xargs" && name != "parallel" {
		return runnerFeed{}
	}
	end := len(argv) - len(rest) // past the program
	for i, a := range argv[:max(end, 0)] {
		var f runnerFeed
		switch progName(a) {
		case "xargs":
			f = xargsFeed(argv[i+1:], known[i+1:])
		case "parallel":
			f = parallelFeed(argv[i+1:], known[i+1:])
		default:
			continue
		}
		return f
	}
	return runnerFeed{}
}

// unsureFeed is f when the runner's options could not be read: input
// from anywhere, every word fed, the input appended.
func unsureFeed(f runnerFeed) runnerFeed {
	f.fromInput, f.appends, f.placeholders = true, true, []string{fedAll}
	return f
}

// xargsShort and xargsLongFlags are the options xargs has (GNU xargs
// 4.9): short ones with a value (a d E I L n P s), with an optional value
// attached (e i l), with none (0 o p r t x); long ones with none, the
// valued ones being the wrapper table's.
const (
	xargsShortValue    = "adEILnPs"
	xargsShortOptional = "eil"
	xargsShortFlag     = "0oprtx"
	xargsNumeric       = "LnPs"
)

var xargsLongFlags = []string{
	"null", "interactive", "no-run-if-empty", "open-tty", "show-limits", "verbose", "exit", "help", "version",
}

// xargsFeed reads xargs's options, in args (the words after xargs, known
// as given): its replacement string -I R, -IR, -i[R] (default {}),
// --replace[=R]; with one, the input replaces the string in the command,
// with none it is appended. The options end at the first word that is
// not one, or at --; the command is the word after, and must be known.
func xargsFeed(args []string, known []bool) runnerFeed {
	f := runnerFeed{runner: "xargs", fromInput: true}
	table := wrappers["xargs"]
	next := func(i int) (string, bool) { // the word after i, known
		if i+1 < len(args) && known[i+1] {
			return args[i+1], true
		}
		return "", false
	}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !known[i] {
			return unsureFeed(f)
		}
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		if strings.HasPrefix(a, "--") {
			name, value, hasValue := strings.Cut(a[2:], "=")
			switch {
			case name == "replace" || name == "replace-str":
				if !hasValue || value == "" {
					value = "{}"
				}
				f.placeholders = append(f.placeholders, value)
			case name == "eof" || name == "max-lines":
				// An optional value, attached only.
			case slices.Contains(table.long, name):
				if !hasValue {
					if _, ok := next(i); !ok {
						return unsureFeed(f)
					}
					i++
				}
			case slices.Contains(xargsLongFlags, name) && !hasValue:
			default:
				return unsureFeed(f)
			}
			continue
		}
		// A cluster of short options, to the first that takes a value.
	cluster:
		for j := 1; j < len(a); j++ {
			c := a[j]
			switch {
			case strings.IndexByte(xargsShortFlag, c) >= 0:
			case strings.IndexByte(xargsShortOptional, c) >= 0:
				if c == 'i' {
					rest := a[j+1:]
					if rest == "" {
						rest = "{}"
					}
					f.placeholders = append(f.placeholders, rest)
				}
				break cluster
			case strings.IndexByte(xargsShortValue, c) >= 0:
				value := a[j+1:]
				if value == "" {
					v, ok := next(i)
					if !ok {
						return unsureFeed(f)
					}
					i++
					value = v
				}
				if strings.IndexByte(xargsNumeric, c) >= 0 && strings.Trim(value, "0123456789") != "" {
					return unsureFeed(f)
				}
				if c == 'I' {
					f.placeholders = append(f.placeholders, value)
				}
				break cluster
			default:
				return unsureFeed(f)
			}
		}
	}
	if i < len(args) && !known[i] {
		return unsureFeed(f) // the command is a word the gate cannot tell
	}
	f.appends = len(f.placeholders) == 0
	return f
}

// parallelFlags and parallelValues are the GNU parallel options the gate
// reads: with none, and with a value (given apart or attached). Any other
// option leaves the runner unread.
var parallelFlags = []string{
	"-k", "--keep-order", "--bar", "--progress", "--eta", "-X", "-m", "--will-cite", "--tag", "-v", "--verbose",
	"--dry-run", "-r", "--no-run-if-empty", "-u", "--ungroup", "-g", "--group", "--line-buffer", "--lb",
	"--linebuffer", "-0", "--null", "--halt-on-error", "--shuf", "-q", "--quote", "--pipe", "--pipepart",
	"--citation", "--no-notice", "--nice", "-t", "--link", "--xargs", "--sshdelay", "--plain", "--bibtex",
}

var parallelValues = []string{
	"-j", "--jobs", "-P", "-S", "--sshlogin", "-a", "--arg-file", "-d", "--delimiter", "-I", "--replace",
	"--replace-str", "-N", "-n", "--max-args", "-L", "-l", "--max-lines", "-s", "--max-chars", "-E", "--eof",
	"--colsep", "-C", "--results", "--res", "--joblog", "--tmpdir", "--timeout", "--halt", "--memfree", "--load",
	"--retries", "--delay", "--tag-string", "--tagstring", "--workdir", "--wd", "--block", "-b", "--header",
	"--recstart", "--recend", "--env", "--sshloginfile", "--slf", "--jobs-file", "--max-procs", "--delay-to",
}

// parallelFeed reads GNU parallel's words after parallel (known as
// given): with ::: (literal arguments) and no :::: or -a, the arguments
// are its own and nothing comes from input; otherwise every input line is
// a word. -I R / --replace R changes {} to R. Placeholders are the
// defaults and R; with none in the command, the input is appended. The
// options end at the first word that is not one, or at ::: ; an option the
// gate does not know, or a word it cannot tell among them or as the
// command, leaves it unread (unsureFeed).
func parallelFeed(args []string, known []bool) runnerFeed {
	f := runnerFeed{runner: "parallel", fromInput: true}
	var replace []string
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !known[i] {
			return unsureFeed(f)
		}
		if strings.HasPrefix(a, ":::") || !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		name, value, hasValue := a, "", false
		if strings.HasPrefix(a, "--") {
			name, value, hasValue = strings.Cut(a, "=")
		} else if len(a) > 2 && slices.Contains(parallelValues, a[:2]) {
			name, value, hasValue = a[:2], a[2:], true // -j4, -IR
		}
		switch {
		case slices.Contains(parallelValues, name):
			if !hasValue {
				if i+1 >= len(args) || !known[i+1] {
					return unsureFeed(f)
				}
				i++
				value = args[i]
			}
			if name == "-I" || name == "--replace" || name == "--replace-str" {
				replace = append(replace, value)
			}
		case slices.Contains(parallelFlags, name) && !hasValue:
		case name == "-i":
			replace = append(replace, "{}")
		default:
			return unsureFeed(f)
		}
	}
	if i < len(args) && !known[i] {
		return unsureFeed(f)
	}
	template := args[min(i, len(args)):]
	for k, a := range template {
		if strings.HasPrefix(a, ":::") {
			template = template[:k]
			break
		}
	}
	literal := false
	for _, a := range args {
		switch {
		case a == "::::" || a == "-a" || a == "--arg-file" || a == "--pipe" || a == "--pipepart" ||
			strings.HasPrefix(a, "--arg-file=") || strings.HasPrefix(a, "-a") && len(a) > 2 && a[1] == 'a' && !strings.HasPrefix(a, "--"):
			literal = false
			f.fromInput = true
			goto parsed
		case strings.HasPrefix(a, ":::"):
			literal = true
		}
	}
parsed:
	if literal {
		f.fromInput = false
		return f
	}
	f.placeholders = slices.Concat(replace, parallelPlaceholders)
	f.appends = !slices.ContainsFunc(template, func(w string) bool { return containsAny(w, f.placeholders) })
	return f
}

// containsAny reports whether s holds one of subs, or subs holds fedAll.
func containsAny(s string, subs []string) bool {
	return slices.ContainsFunc(subs, func(p string) bool { return p == fedAll || p != "" && strings.Contains(s, p) })
}

// carriesLines are the commands whose words are command lines read in
// turn, where a runner's placeholder is judged as the line is read.
func carriesLines(name string) bool {
	switch name {
	case "eval", "watch", "trap", "parallel", "find", "fd", "fdfind", "tmux", "at", "batch", "script", "flock":
		return true
	}
	return false
}

// markFed marks the words of v after its program that hold a placeholder
// (phs) as words the gate cannot tell, standing for input words (fed): a
// reader or search given one touches every root. A shell's command
// string is left to the line it holds, which is read with the same
// placeholders. known is the expanded words' known flags, shared with
// v.argvKnown and v.restKnown; first is the index in them of the first
// word of v.argv.
func markFed(v *view, known []bool, first int, phs []string) {
	carries := carriesLines(v.name)
	skip := -1
	if shells[v.name] {
		skip = shellString(v.rest)
	}
	k, at := 0, 0
	for wi := range v.words {
		if v.words[wi].redirect {
			// A file an input redirection names is read by whatever the
			// redirection is for, so a placeholder there stands for a
			// file name too.
			if w := &v.words[wi]; w.input && !w.here && containsAny(w.text, phs) {
				w.known, w.fed = false, true
			}
			continue
		}
		if carries {
			continue
		}
		pos := k - first - (len(v.argv) - len(v.rest))
		k++
		at = wi
		if pos < 0 || pos == skip || !containsAny(v.words[at].text, phs) {
			continue
		}
		v.words[at].known, v.words[at].fed = false, true
		known[k-1] = false
	}
}

// isInterpreter reports whether name is a script interpreter the gate
// knows (interpreters): one that reads the files it is given as programs
// or data.
func isInterpreter(name string) bool {
	_, ok := interpreters[interpreterFamily(name)]
	return ok
}
