package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/pbir"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
	"github.com/craigalodon/pbipx/internal/source"
	"github.com/craigalodon/pbipx/internal/sqlfile"
	"github.com/craigalodon/pbipx/internal/tmdl"
)

// notExplained is the kind of a file explain does not describe.
const notExplained = "not-explained"

type explainFlags struct {
	format string
	policy string
}

func newExplainCmd() *cobra.Command {
	var f explainFlags
	cmd := &cobra.Command{
		Use:   "explain <paths...>",
		Short: "print the structure of reports, models and SQL, never a value",
		Long: `Print the structure of the files given, so that a protected file never
needs to be read to learn its shape: a report's contexts, a model's tables,
columns, measures, partitions, relationships, roles and expressions, and the
relations SQL creates, writes and reads. A directory expands to the files
under it. Names with a personal-data shape, and names read from strings that
are not plain identifiers, print as placeholders. Values are never printed,
and no flag prints them.

Exit codes: 0 described, 3 a file that does not parse or cannot be read, or a
failure, 4 a usage error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(cmd, args, &f)
		},
	}
	cmd.Flags().StringVar(&f.format, "format", formatHuman, "output format: human or json")
	cmd.Flags().StringVar(&f.policy, "policy", "", "the policy file (default: .pbipx.toml at the repository root)")
	return cmd
}

// explained is one file's entry: its path, kind, and either its error
// reason or the description of its kind, whose sections are written at the
// top level of the entry. A file not explained has neither.
type explained struct {
	Path  string
	Kind  string
	Error string
	// Description is a *pbir.Description, *tmdl.Description or
	// *sqlfile.Description; nil with an error or for a file not explained.
	Description any
}

// MarshalJSON writes path, kind, then the error or the description's own
// members, in their order.
func (e *explained) MarshalJSON() ([]byte, error) {
	head := struct {
		Path  string `json:"path"`
		Kind  string `json:"kind"`
		Error string `json:"error,omitempty"`
	}{e.Path, e.Kind, e.Error}
	out, err := marshalNoEscape(head)
	if err != nil || e.Description == nil {
		return out, err
	}
	body, err := marshalNoEscape(e.Description)
	if err != nil {
		return nil, err
	}
	body = bytes.TrimSpace(body)
	if len(body) <= 2 || body[0] != '{' {
		return out, nil
	}
	out = append(out[:len(out)-1], ',')
	return append(out, body[1:]...), nil
}

// marshalNoEscape is json.Marshal without HTML escaping, so a placeholder
// such as <withheld> reads as written.
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}

func runExplain(cmd *cobra.Command, args []string, f *explainFlags) error {
	if len(args) == 0 {
		return usageError{"explain needs a file or a directory"}
	}
	if f.format != formatHuman && f.format != formatJSON {
		return usageError{"--format must be human or json"}
	}
	cwd, repo, err := openRepo(cmd, f.format)
	if err != nil {
		return err
	}
	p, err := loadPolicy(cmd, f.format, cwd, repo, f.policy)
	if err != nil {
		return err
	}
	set, err := repo.Paths(cwd, args)
	if err != nil {
		return explainSourceError(cmd, err)
	}
	defer func() { _ = set.Close() }()

	files := explainFiles(set.Paths, set, p)
	code := finding.ExitClean
	for _, e := range files {
		if e.Error != "" {
			code = finding.ExitOperational
		}
	}
	var werr error
	if f.format == formatJSON {
		werr = writeExplainJSON(cmd.OutOrStdout(), files)
	} else {
		werr = writeExplainHuman(cmd.OutOrStdout(), files)
	}
	if werr != nil {
		return operational(cmd, "writing the result failed")
	}
	return exitWith(code)
}

// explainSourceError is a path that cannot be explained: a usage error that
// names the path by its position, never by what was typed.
func explainSourceError(cmd *cobra.Command, err error) error {
	var pe *source.PathError
	if errors.As(err, &pe) {
		return usageError{pe.Error()}
	}
	return operational(cmd, gitDetail(err))
}

// explainFiles describes each path, in order. Every model file is parsed
// before any is described, since a partition can read through a parameter
// another file defines.
func explainFiles(paths []string, r scan.Reader, p *policy.Policy) []*explained {
	out := make([]*explained, len(paths))
	var models []int
	var roots []*tmdl.Node
	for i, path := range paths {
		e := &explained{Path: path, Kind: notExplained}
		out[i] = e
		class, kind := p.Classify(path)
		if class != policy.Scanned || (kind != policy.Report && kind != policy.Model && kind != policy.SQL) {
			continue
		}
		e.Kind = string(kind)
		data, reason := readForExplain(r, path)
		if reason != "" {
			e.Error = reason
			continue
		}
		switch kind {
		case policy.Report:
			d, err := safely(func() (*pbir.Description, error) { return pbir.Describe(data) })
			if err != nil {
				e.Error = errorReason(err)
				continue
			}
			e.Description = d
		case policy.SQL:
			d, err := safely(func() (*sqlfile.Description, error) { return sqlfile.Describe(data) })
			if err != nil {
				e.Error = errorReason(err)
				continue
			}
			e.Description = d
		case policy.Model:
			root, err := safely(func() (*tmdl.Node, error) { return tmdl.Parse(data) })
			if err != nil {
				e.Error = errorReason(err)
				continue
			}
			models = append(models, i)
			roots = append(roots, root)
		}
	}
	descs, errs := describeModels(roots)
	for k, i := range models {
		if errs[k] != nil {
			out[i].Error = errorReason(errs[k])
			continue
		}
		out[i].Description = descs[k]
	}
	return out
}

// describeModels describes the parsed model files together. A panic is an
// unvettable error for every one of them.
func describeModels(roots []*tmdl.Node) (descs []*tmdl.Description, errs []error) {
	defer func() {
		if recover() != nil {
			descs = make([]*tmdl.Description, len(roots))
			errs = make([]error, len(roots))
			for i := range errs {
				errs[i] = errors.New("describing the model failed")
			}
		}
	}()
	return tmdl.Describe(roots)
}

// safely runs a describer, turning a panic into an error: never a clean
// description, and never its message, which could quote the file.
func safely[T any](fn func() (T, error)) (v T, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			v, err = zero, errors.New("describing the file failed")
		}
	}()
	return fn()
}

// readForExplain reads a file to describe, as scan reads one: content that
// is not UTF-8 is an error, and a byte order mark is removed. The second
// result is an error reason, or "".
func readForExplain(r scan.Reader, path string) ([]byte, string) {
	data, err := r.Read(path)
	if err != nil {
		return nil, string(finding.Unreadable)
	}
	if !utf8.Valid(data) {
		return nil, string(finding.NotUTF8)
	}
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), ""
}

// errorReason is a describer's error as an error reason of the vocabulary:
// a parse error's own, and unvettable for anything else.
func errorReason(err error) string {
	var pe *judge.ParseError
	if errors.As(err, &pe) {
		for _, r := range finding.ErrorReasons {
			if pe.Reason == r {
				return string(r)
			}
		}
	}
	return string(finding.Unvettable)
}

func writeExplainJSON(w io.Writer, files []*explained) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(struct {
		Files []*explained `json:"files"`
	}{files})
}

// writeExplainHuman writes the JSON content as indented text: each file's
// path and kind, then its sections, in the same order.
func writeExplainHuman(w io.Writer, files []*explained) error {
	var b strings.Builder
	for i, e := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		v, err := decodeOrdered(raw)
		if err != nil {
			return err
		}
		obj, _ := v.(ordered)
		fmt.Fprintf(&b, "%s  (%s)\n", e.Path, e.Kind)
		for _, m := range obj {
			if m.key == "path" || m.key == "kind" {
				continue
			}
			writeHumanValue(&b, m.key, m.val, 1)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// ordered is a JSON object with its members in order.
type ordered []orderedMember

type orderedMember struct {
	key string
	val any
}

func decodeOrdered(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return decodeOrderedValue(dec)
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		var obj ordered
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			key, _ := k.(string)
			obj = append(obj, orderedMember{key: key, val: v})
		}
		_, err := dec.Token()
		return obj, err
	case json.Delim('['):
		arr := []any{}
		for dec.More() {
			v, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		_, err := dec.Token()
		return arr, err
	}
	return tok, nil
}

// writeHumanValue writes one member at a depth: a scalar on its line, an
// object's members indented below it, and a list one item per line.
func writeHumanValue(b *strings.Builder, key string, v any, depth int) {
	pad := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case ordered:
		if len(t) == 0 {
			fmt.Fprintf(b, "%s%s: none\n", pad, key)
			return
		}
		fmt.Fprintf(b, "%s%s:\n", pad, key)
		for _, m := range t {
			writeHumanValue(b, m.key, m.val, depth+1)
		}
	case []any:
		if len(t) == 0 {
			fmt.Fprintf(b, "%s%s: none\n", pad, key)
			return
		}
		fmt.Fprintf(b, "%s%s:\n", pad, key)
		for _, item := range t {
			obj, ok := item.(ordered)
			if !ok {
				fmt.Fprintf(b, "%s  - %s\n", pad, scalarText(item))
				continue
			}
			for k, m := range obj {
				lead := "    "
				if k == 0 {
					lead = "  - "
				}
				if _, nested := m.val.(ordered); nested {
					fmt.Fprintf(b, "%s%s%s:\n", pad, lead, m.key)
					for _, mm := range m.val.(ordered) {
						writeHumanValue(b, mm.key, mm.val, depth+3)
					}
					continue
				}
				if arr, isArr := m.val.([]any); isArr {
					var sub strings.Builder
					writeHumanValue(&sub, m.key, arr, 0)
					for j, line := range strings.Split(strings.TrimSuffix(sub.String(), "\n"), "\n") {
						if j == 0 {
							fmt.Fprintf(b, "%s%s%s\n", pad, lead, line)
						} else {
							fmt.Fprintf(b, "%s    %s\n", pad, line)
						}
					}
					continue
				}
				fmt.Fprintf(b, "%s%s%s: %s\n", pad, lead, m.key, scalarText(m.val))
			}
		}
	default:
		fmt.Fprintf(b, "%s%s: %s\n", pad, key, scalarText(v))
	}
}

func scalarText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case nil:
		return "none"
	}
	return fmt.Sprint(v)
}
