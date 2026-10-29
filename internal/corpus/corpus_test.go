package corpus_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
)

// corpusDir is the behavior corpus (docs/design/corpus.md).
const corpusDir = "../../testdata/corpus"

// TestCorpusValid holds every case to the corpus format.
func TestCorpusValid(t *testing.T) {
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range c.Problems() {
		t.Error(e)
	}
	for _, cs := range c.Cases {
		t.Run(cs.ID(), func(t *testing.T) {
			for _, e := range c.Validate(cs) {
				t.Error(e)
			}
		})
	}
}
