package corpus

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// Commands a case may run.
const (
	commandScan     = "scan"
	commandScanText = "scan-text"
	commandGate     = "gate"
	commandExplain  = "explain"
	commandBaseline = "baseline"
)

var commands = map[string]bool{
	commandScan: true, commandScanText: true, commandGate: true,
	commandExplain: true, commandBaseline: true,
}

// Areas are the corpus's case groups (docs/design/corpus.md, "Layout").
var Areas = []string{"pbir", "tmdl", "sql", "yaml", "text", "baseline", "policy", "gate", "explain"}

// Files in a case directory.
const (
	caseFile     = "case.toml"
	expectedFile = "expected.json"
	stdinFile    = "stdin"
	treeDir      = "tree"
	afterDir     = "after"

	policyFile     = ".pbipx.toml"
	policyInTree   = "tree"
	vocabularyFile = "vocabulary.toml"
	policiesDir    = "policies"
)

// exitUsage is the usage-error exit code: such a case has no expected.json.
const exitUsage = 4

var (
	kebab    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	plainKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// Spec is a case.toml.
type Spec struct {
	Description string            `toml:"description"`
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Policy      string            `toml:"policy"`
	Exit        int               `toml:"exit"`
	Sentinels   []string          `toml:"sentinels"`
	Git         *bool             `toml:"git"`
	Untracked   []string          `toml:"untracked"`
	Staged      []string          `toml:"staged"`
	Env         map[string]string `toml:"env"`
	Known       string            `toml:"known"`
	// Approved, for a gate case, is false to run without an approval of the
	// policy; a gate case otherwise runs against an approved one.
	Approved *bool `toml:"approved"`

	// Baseline and Fingerprint describe findings the runner resolves to
	// fingerprints (corpus.md, "Baseline cases").
	Baseline    []BaselineEntry `toml:"baseline"`
	Fingerprint *FindingRef     `toml:"fingerprint"`
}

// FindingRef describes one finding by what every implementation agrees on.
// A nil Column is a null column.
type FindingRef struct {
	Path    string   `toml:"path"`
	Context string   `toml:"context"`
	Column  *string  `toml:"column"`
	Reasons []string `toml:"reasons"`
}

// BaselineEntry is one [[baseline]] entry: a finding, and the severity and
// expiry its baseline entry is written with, as given.
type BaselineEntry struct {
	Path     string   `toml:"path"`
	Context  string   `toml:"context"`
	Column   *string  `toml:"column"`
	Reasons  []string `toml:"reasons"`
	Severity string   `toml:"severity"`
	Expires  string   `toml:"expires"`
}

// Ref is the finding the entry describes.
func (e *BaselineEntry) Ref() FindingRef {
	return FindingRef{Path: e.Path, Context: e.Context, Column: e.Column, Reasons: e.Reasons}
}

// NeedsResolve reports whether the case describes findings that the runner
// must resolve before the command runs.
func (s *Spec) NeedsResolve() bool { return len(s.Baseline) > 0 || s.Fingerprint != nil }

// HasResult reports whether the case is meant to have an expected.json: every
// gate case, and every other case except a usage error and baseline record
// and verify.
func (s *Spec) HasResult() bool {
	if s.Command == commandGate {
		return true
	}
	if s.Command == commandBaseline && len(s.Args) > 0 && (s.Args[0] == "record" || s.Args[0] == "verify") {
		return false
	}
	return s.Exit != exitUsage
}

// requiredKeys must appear in every case.toml.
var requiredKeys = []string{"description", "command", "args", "policy", "exit", "sentinels"}

// Approves reports whether the runner approves the case's policy before the
// command runs: a gate case, unless approved = false.
func (s *Spec) Approves() bool {
	return s.Command == commandGate && (s.Approved == nil || *s.Approved)
}

// UsesGit reports whether the case's root is a git repository.
func (s *Spec) UsesGit() bool { return s.Git == nil || *s.Git }

// Case is one corpus case, as loaded. A case that failed to load keeps its
// problems, which Validate reports.
type Case struct {
	Area string
	Name string
	Dir  string

	Spec Spec
	// Expected is the parsed expected.json, nil when there is none.
	Expected *Result
	HasStdin bool
	Stdin    []byte
	HasTree  bool
	// HasAfter is set when the case has after/tree/, copied over the root
	// once its findings are resolved and its policy approved.
	HasAfter bool

	// expectedRaw is expected.json as read, for checks on its text.
	expectedRaw []byte
	problems    []error
	validated   bool
	// specOK is set when case.toml decoded with every required key.
	specOK bool
}

// ID names the case as area/case.
func (c *Case) ID() string { return c.Area + "/" + c.Name }

// TreeDir is the case's tree/ directory.
func (c *Case) TreeDir() string { return filepath.Join(c.Dir, treeDir) }

// AfterTreeDir is the case's after/tree/ directory.
func (c *Case) AfterTreeDir() string { return filepath.Join(c.Dir, afterDir, treeDir) }

func (c *Case) addf(file, format string, args ...any) {
	where := c.ID()
	if file != "" {
		where += ": " + file
	}
	c.problems = append(c.problems, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, args...)))
}

// Corpus is a loaded corpus directory.
type Corpus struct {
	Dir   string
	Vocab *Vocabulary
	Cases []*Case

	// problems are layout problems that belong to no one case.
	problems []error
}

// Open loads the vocabulary and every case under dir. A malformed case is
// still returned; Validate reports its problems.
func Open(dir string) (*Corpus, error) {
	return open(dir, filepath.Join(dir, vocabularyFile))
}

// open is Open with the vocabulary read from elsewhere, for tests.
func open(dir, vocabPath string) (*Corpus, error) {
	vocab, err := LoadVocabulary(vocabPath)
	if err != nil {
		return nil, err
	}
	c := &Corpus{Dir: dir, Vocab: vocab}
	known := make(map[string]bool, len(Areas))
	for _, a := range Areas {
		known[a] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("corpus: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == policiesDir {
			continue
		}
		area := e.Name()
		if !known[area] {
			c.problems = append(c.problems, fmt.Errorf("corpus: directory %q is not an area", area))
			continue
		}
		cases, err := os.ReadDir(filepath.Join(dir, area))
		if err != nil {
			return nil, fmt.Errorf("corpus: %w", err)
		}
		for _, ce := range cases {
			if !ce.IsDir() {
				c.problems = append(c.problems, fmt.Errorf("corpus: %s: %q is not a case directory", area, ce.Name()))
				continue
			}
			c.Cases = append(c.Cases, loadCase(filepath.Join(dir, area, ce.Name()), area, ce.Name(), vocab))
		}
	}
	sort.Slice(c.Cases, func(i, j int) bool { return c.Cases[i].ID() < c.Cases[j].ID() })
	return c, nil
}

// loadCase reads one case directory. Problems are recorded on the case.
func loadCase(dir, area, name string, vocab *Vocabulary) *Case {
	c := &Case{Area: area, Name: name, Dir: dir}
	if !kebab.MatchString(name) {
		c.addf("", "the case directory name is not kebab case")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		c.addf("", "cannot read the case directory")
		return c
	}
	for _, e := range entries {
		switch e.Name() {
		case caseFile, expectedFile, stdinFile:
			if e.IsDir() {
				c.addf(e.Name(), "is a directory, not a file")
			}
		case treeDir:
			if !e.IsDir() {
				c.addf(treeDir, "is not a directory")
			}
			c.HasTree = e.IsDir()
		case afterDir:
			c.loadAfter(dir)
		default:
			c.addf("", "unexpected entry %q (a case holds case.toml, expected.json, stdin, tree/ and after/tree/)", e.Name())
		}
	}

	data, err := readIn(dir, caseFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.addf(caseFile, "missing")
	case err != nil:
		c.addf(caseFile, "cannot be read")
	default:
		c.decodeSpec(data)
	}

	if data, err := readIn(dir, stdinFile); err == nil {
		c.HasStdin, c.Stdin = true, data
	} else if !errors.Is(err, fs.ErrNotExist) {
		c.addf(stdinFile, "cannot be read")
	}

	data, err = readIn(dir, expectedFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		c.addf(expectedFile, "cannot be read")
	default:
		c.expectedRaw = data
		res, problems := ParseResult(data, c.Spec.Command, vocab, true)
		for _, p := range problems {
			c.addf(expectedFile, "%s", p)
		}
		if len(problems) == 0 {
			c.Expected = res
		}
	}
	return c
}

// loadAfter checks that after/ holds exactly a tree/ directory.
func (c *Case) loadAfter(dir string) {
	entries, err := os.ReadDir(filepath.Join(dir, afterDir))
	if err != nil {
		c.addf(afterDir, "is not a readable directory")
		return
	}
	for _, e := range entries {
		if e.Name() != treeDir || !e.IsDir() {
			c.addf(afterDir, "unexpected entry %q (after/ holds only tree/)", e.Name())
			continue
		}
		c.HasAfter = true
	}
	if !c.HasAfter {
		c.addf(afterDir, "has no tree/")
	}
}

// decodeSpec parses case.toml: first loosely, to find missing keys, then
// strictly, to reject unknown ones.
func (c *Case) decodeSpec(data []byte) {
	var loose map[string]any
	if err := toml.Unmarshal(data, &loose); err != nil {
		c.addf(caseFile, "%s", tomlProblem(err))
		return
	}
	ok := true
	for _, k := range requiredKeys {
		if _, present := loose[k]; !present {
			c.addf(caseFile, "required key %q is missing", k)
			ok = false
		}
	}
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := dec.Decode(&c.Spec); err != nil {
		c.addf(caseFile, "%s", tomlProblem(err))
		ok = false
	}
	c.specOK = ok
}
