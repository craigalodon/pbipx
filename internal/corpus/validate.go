package corpus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// knownPredecessor is the one value of `known` (corpus.md, "Known
// differences").
const knownPredecessor = "predecessor"

var (
	envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// plantsNothing is how a description says that its case plants no value,
	// which an empty sentinels list requires. It accepts "plants no value",
	// "plants nothing", "no value is planted", "nothing planted", "no values"
	// and the like: a negation followed, in the same clause, by "value" or
	// "plant".
	plantsNothing = regexp.MustCompile(`(?i)\b(no|nothing|none|without)\b[^.;:]*\b(values?|plant(s|ed)?)\b|\bplants? (no|nothing)\b`)
)

// Problems reports layout problems that belong to no one case, such as a
// directory that is not an area.
func (c *Corpus) Problems() []error { return c.problems }

// Validate checks a case against docs/design/corpus.md and the vocabulary.
// Each error names the case, the file and the rule, never a fixture value.
func (c *Corpus) Validate(cs *Case) []error {
	if !cs.validated {
		cs.validated = true
		v := &validator{corpus: c, cs: cs}
		v.run()
	}
	return cs.problems
}

type validator struct {
	corpus *Corpus
	cs     *Case
	// files are the regular files of tree/ and after/tree/.
	files []treeFile
}

// treeFile is one fixture file. rel is its path in the root; label names it
// in a problem, from the case directory.
type treeFile struct {
	rel, label string
	data       []byte
}

func (v *validator) addf(file, format string, args ...any) { v.cs.addf(file, format, args...) }

func (v *validator) run() {
	cs, s := v.cs, &v.cs.Spec
	if !cs.specOK {
		// The rest depends on case.toml; its problems are already recorded.
		return
	}
	v.readTree()

	if strings.TrimSpace(s.Description) == "" {
		v.addf(caseFile, "description is empty")
	}
	if !commands[s.Command] {
		v.addf(caseFile, "command is not one of scan, scan-text, gate, explain, baseline")
	}
	if s.Known != "" && s.Known != knownPredecessor {
		v.addf(caseFile, "known is set to something other than %q", knownPredecessor)
	}
	for k := range s.Env {
		if !envName.MatchString(k) {
			v.addf(caseFile, "env has a key that is not an environment variable name")
		}
		if k == configDirEnv {
			v.addf(caseFile, "env sets %s, which the runner owns", configDirEnv)
		}
	}
	if s.Approved != nil && s.Command != commandGate {
		v.addf(caseFile, "approved is set, but only a gate case's policy is approved")
	}
	if s.Command != commandGate && s.HasResult() {
		for i, a := range s.Args {
			if a == "--format" || strings.HasPrefix(a, "--format=") {
				v.addf(caseFile, "args[%d] sets --format, which the runner owns", i)
			}
		}
	}
	if s.Command == commandGate && !cs.HasStdin {
		v.addf(stdinFile, "missing: a gate case needs its hook event")
	}

	v.checkPolicy()
	v.checkBaseline()
	v.checkGit()
	v.checkExit()
	v.checkSentinels()
	v.checkReportFixtures()
	v.checkFindingPaths()
}

// readTree loads every regular file under tree/ and after/tree/.
func (v *validator) readTree() {
	if v.cs.HasTree {
		v.readDir(v.cs.TreeDir(), treeDir)
	}
	if v.cs.HasAfter {
		v.readDir(v.cs.AfterTreeDir(), afterDir+"/"+treeDir)
	}
}

func (v *validator) readDir(dir, label string) {
	tree := os.DirFS(dir)
	err := fs.WalkDir(tree, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := fs.ReadFile(tree, rel)
		if err != nil {
			return err
		}
		v.files = append(v.files, treeFile{rel: rel, label: label + "/" + rel, data: data})
		return nil
	})
	if err != nil {
		v.addf(label, "cannot be read")
	}
}

// inTree reports whether a slash path exists under tree/, as a file,
// directory or link.
func (v *validator) inTree(rel string) bool {
	return v.cs.HasTree && existsIn(v.cs.TreeDir(), rel)
}

// inTrees reports whether a slash path exists under tree/ or after/tree/.
func (v *validator) inTrees(rel string) bool {
	return v.inTree(rel) || (v.cs.HasAfter && existsIn(v.cs.AfterTreeDir(), rel))
}

func existsIn(dir, rel string) bool {
	if !validRelPath(rel) {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func (v *validator) checkPolicy() {
	s := &v.cs.Spec
	treePolicy := v.inTree(policyFile)
	switch {
	case s.Policy == policyInTree:
		// A case may leave the policy out to show that a missing one fails.
		missingOK := v.cs.Expected != nil && v.cs.Expected.Failure == "policy-error"
		if !treePolicy && !missingOK {
			v.addf(caseFile, "policy is %q but tree/%s does not exist", policyInTree, policyFile)
		}
	case !kebab.MatchString(s.Policy):
		v.addf(caseFile, "policy is not a policy name or %q", policyInTree)
	default:
		p := filepath.Join(v.corpus.Dir, policiesDir, s.Policy+".toml")
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			v.addf(caseFile, "policy %q names no file in %s/", s.Policy, policiesDir)
		}
		if treePolicy {
			v.addf(caseFile, "tree/%s exists but policy is not %q; the runner would overwrite it", policyFile, policyInTree)
		}
	}
}

// checkBaseline checks [[baseline]], [fingerprint] and after/tree/
// (corpus.md, "Baseline cases").
func (v *validator) checkBaseline() {
	s := &v.cs.Spec
	for i := range s.Baseline {
		e := &s.Baseline[i]
		what := fmt.Sprintf("baseline[%d]", i)
		v.checkRef(e.Ref(), what)
		if e.Severity == "" {
			v.addf(caseFile, "%s.severity is missing", what)
		}
		if e.Expires == "" {
			v.addf(caseFile, "%s.expires is missing", what)
		}
	}
	if s.Fingerprint != nil {
		v.checkRef(*s.Fingerprint, "fingerprint")
	}
	uses := slices.ContainsFunc(s.Args, func(a string) bool { return strings.Contains(a, fingerprintPlaceholder) })
	switch {
	case uses && s.Fingerprint == nil:
		v.addf(caseFile, "args use %s, but there is no [fingerprint]", fingerprintPlaceholder)
	case !uses && s.Fingerprint != nil:
		v.addf(caseFile, "[fingerprint] is set, but no argument uses %s", fingerprintPlaceholder)
	}
	if v.cs.HasAfter && !s.NeedsResolve() && !s.Approves() {
		v.addf(afterDir, "is applied only in a case with [[baseline]] or [fingerprint], or a gate case whose policy is approved")
	}
}

// checkRef checks a finding description. It must name a finding of tree/,
// which the setup scan reads before after/tree/ is applied.
func (v *validator) checkRef(r FindingRef, what string) {
	switch {
	case !validRelPath(r.Path):
		v.addf(caseFile, "%s.path is not a clean relative path with forward slashes", what)
	case !v.inTree(r.Path):
		v.addf(caseFile, "%s.path names no path in tree/", what)
	}
	if _, ok := v.corpus.Vocab.Contexts[r.Context]; !ok {
		v.addf(caseFile, "%s.context is not one of the vocabulary's contexts", what)
	}
	if r.Column != nil && *r.Column == "" {
		v.addf(caseFile, "%s.column is empty (omit it for a null column)", what)
	}
	if len(r.Reasons) == 0 {
		v.addf(caseFile, "%s.reasons is empty", what)
	}
	last := -1
	for j, reason := range r.Reasons {
		rank, ok := v.corpus.Vocab.reasonRank[reason]
		if !ok {
			v.addf(caseFile, "%s.reasons[%d] is not one of the vocabulary's finding reasons", what, j)
			return
		}
		if rank <= last {
			v.addf(caseFile, "%s.reasons is not in the vocabulary's reason-order, or repeats one", what)
			return
		}
		last = rank
	}
}

func (v *validator) checkGit() {
	s := &v.cs.Spec
	if !s.UsesGit() && (len(s.Untracked) > 0 || len(s.Staged) > 0) {
		v.addf(caseFile, "untracked or staged is set but git = false")
	}
	seen := map[string]string{}
	for _, list := range []struct {
		key   string
		paths []string
	}{{"untracked", s.Untracked}, {"staged", s.Staged}} {
		for i, p := range list.paths {
			switch {
			case !validRelPath(p):
				v.addf(caseFile, "%s[%d] is not a clean relative path with forward slashes", list.key, i)
			case !v.inTree(p):
				v.addf(caseFile, "%s[%d] names no path in tree/", list.key, i)
			}
			if prev, dup := seen[p]; dup {
				v.addf(caseFile, "%s[%d] is already listed in %s", list.key, i, prev)
			}
			seen[p] = list.key
		}
	}
}

func (v *validator) checkExit() {
	cs, s, vocab := v.cs, &v.cs.Spec, v.corpus.Vocab
	if !commands[s.Command] {
		return
	}
	if !vocab.validExit(s.Command, s.Exit) {
		if s.Command == commandGate {
			v.addf(caseFile, "exit is not one of the vocabulary's gate-exit-codes")
		} else {
			v.addf(caseFile, "exit is not one of the vocabulary's exit-codes")
		}
		return
	}
	hasExpected := cs.expectedRaw != nil
	switch {
	case s.Command == commandGate && !hasExpected:
		v.addf(expectedFile, "missing: a gate case needs a decision")
	case hasExpected && !s.HasResult():
		v.addf(expectedFile, "present, but a usage error, baseline record or baseline verify case has none")
	case !hasExpected && s.HasResult():
		v.addf(expectedFile, "missing: only a usage error, baseline record or baseline verify case has none")
	}
	res := cs.Expected
	if res == nil {
		return
	}
	if s.Command == commandGate {
		want := map[string]int{decisionAllow: 0, decisionDeny: 2}[res.Decision]
		if s.Exit != want {
			v.addf(expectedFile, "the decision does not match exit (allow exits 0, deny 2)")
		}
		return
	}
	if res.Failure != "" {
		if s.Exit != 3 {
			v.addf(expectedFile, "a failure exits 3")
		}
		return
	}
	var block, errs bool
	for _, f := range res.Findings {
		block = block || f.Tier == tierBlock
		errs = errs || f.Tier == tierError
	}
	switch {
	case errs && s.Exit != 3:
		v.addf(expectedFile, "an error finding exits 3")
	case !errs && block && s.Exit != 1:
		v.addf(expectedFile, "a block finding, with no error finding, exits 1")
	case s.Exit == 3 && !errs && s.Command != commandExplain:
		v.addf(expectedFile, "exit 3 needs an error finding or a failure")
	case s.Exit == 1 && !block && s.Command != commandExplain:
		v.addf(expectedFile, "exit 1 needs a block finding")
	}
}

func (v *validator) checkSentinels() {
	cs, s := v.cs, &v.cs.Spec
	if len(s.Sentinels) == 0 {
		if !plantsNothing.MatchString(s.Description) {
			v.addf(caseFile, "sentinels is empty, and the description does not say the case plants no value")
		}
		return
	}
	args := []byte(strings.Join(s.Args, "\x00"))
	for i, sen := range s.Sentinels {
		if sen == "" {
			v.addf(caseFile, "sentinels[%d] is empty", i)
			continue
		}
		if slices.Index(s.Sentinels, sen) < i {
			v.addf(caseFile, "sentinels[%d] repeats an earlier sentinel", i)
		}
		found := planted(cs.Stdin, "", sen) || planted(args, "", sen)
		for _, f := range v.files {
			found = found || planted(f.data, f.rel, sen)
		}
		if !found {
			v.addf(caseFile, "sentinels[%d] appears nowhere in tree/, after/tree/, stdin or args", i)
		}
		if containsAny(cs.expectedRaw, sentinelForms(sen)) {
			v.addf(expectedFile, "contains sentinels[%d]; a normalized result never holds a value", i)
		}
		if strings.Contains(s.Description, sen) {
			v.addf(caseFile, "the description contains sentinels[%d]", i)
		}
	}
}

// isReportFixture reports whether a tree path is report JSON: a .json or
// .pbir file below a directory named *.Report.
func isReportFixture(rel string) bool {
	ext := path.Ext(rel)
	if ext != ".json" && ext != ".pbir" {
		return false
	}
	dirs := strings.Split(path.Dir(rel), "/")
	return slices.ContainsFunc(dirs, func(d string) bool { return strings.HasSuffix(d, ".Report") })
}

// checkReportFixtures holds report JSON to Desktop's serialization: it
// parses, and has no final newline. A file the case expects to be an error
// (an error finding on its path, or a gate deny for scan-error) need not
// parse.
func (v *validator) checkReportFixtures() {
	errPaths := map[string]bool{}
	exemptAll := false
	if res := v.cs.Expected; res != nil {
		for _, f := range res.Findings {
			if f.Tier == tierError {
				errPaths[f.Path] = true
			}
		}
		exemptAll = res.Reason == "scan-error"
	}
	for _, f := range v.files {
		if !isReportFixture(f.rel) {
			continue
		}
		if bytes.HasSuffix(f.data, []byte("\n")) {
			v.addf(f.label, "report JSON ends with a newline; Desktop writes none")
		}
		if !errPaths[f.rel] && !exemptAll && !json.Valid(f.data) {
			v.addf(f.label, "report JSON does not parse, and the case expects no error for it")
		}
	}
}

// checkFindingPaths requires every expected finding to name a path in
// tree/ or after/tree/, or the policy file the runner writes. A scan-text
// case with a stdin file may also report "-", standard input.
func (v *validator) checkFindingPaths() {
	res := v.cs.Expected
	if res == nil {
		return
	}
	stdinOK := v.cs.Spec.Command == commandScanText && v.cs.HasStdin
	for i, f := range res.Findings {
		if f.Path == stdinPath && stdinOK {
			continue
		}
		if f.Path != policyFile && !v.inTrees(f.Path) {
			v.addf(expectedFile, "findings[%d].path names no path in tree/ or after/tree/", i)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
