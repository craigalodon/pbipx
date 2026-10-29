package tmdl_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
	"github.com/craigalodon/pbipx/internal/tmdl"
)

const corpusDir = "../../testdata/corpus"

// parsesBeyondTheParser names the cases whose unparseable-tmdl finding comes
// from a later stage, not from this parser. Their files are well-formed TMDL:
// an "Enter data" blob that is not base64 is rejected when the scanner
// decodes it, and an unterminated DAX string when the lexer reads the
// expression. At this level they must parse.
var parsesBeyondTheParser = map[string]bool{
	"enter-data-invalid-base64-is-error": true,
	"unterminated-dax-string-is-error":   true,
}

// TestCorpusFiles parses every .tmdl file in the tmdl cases. A file with an
// unparseable-tmdl finding in its case's expected.json must fail with that
// reason; every other file must parse.
func TestCorpusFiles(t *testing.T) {
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, cs := range c.Cases {
		if cs.Area != "tmdl" || !cs.HasTree {
			continue
		}
		t.Run(cs.Name, func(t *testing.T) {
			failing := map[string]bool{}
			if cs.Expected != nil && !parsesBeyondTheParser[cs.Name] {
				for _, f := range cs.Expected.Findings {
					if slices.Contains(f.Reasons, string(finding.UnparseableTMDL)) {
						failing[f.Path] = true
					}
				}
			}
			dir := cs.TreeDir()
			err := filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(full, ".tmdl") {
					return err
				}
				rel, err := filepath.Rel(dir, full)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				data, err := os.ReadFile(full) //nolint:gosec // G304: a path under the corpus directory
				if err != nil {
					return err
				}
				ran++
				_, perr := tmdl.Parse(data)
				var pe *judge.ParseError
				switch {
				case failing[rel] && !errors.As(perr, &pe):
					t.Errorf("%s: parsed, want an error", rel)
				case failing[rel] && pe.Reason != finding.UnparseableTMDL:
					t.Errorf("%s: reason %q", rel, pe.Reason)
				case !failing[rel] && perr != nil:
					t.Errorf("%s: %v", rel, perr)
				}
				delete(failing, rel)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for rel := range failing {
				t.Errorf("%s: expected to fail but not in the tree", rel)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no tmdl files parsed")
	}
}
