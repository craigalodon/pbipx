package yamlfile

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

func lines(cs []comment) []int {
	var out []int
	for _, c := range cs {
		out = append(out, c.line)
	}
	return out
}

// The scanner alone, without the parser's comments, finds each comment on
// the lines given, and nothing inside a scalar.
func TestCommentScanner(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		lines []int
	}{
		{"quote on a plain continuation", "key: plain\n  'continued # Doe, Jane\nother: 1 # Roe, Richard\n", []int{2, 3}},
		{"double quote on a plain continuation", "key: plain\n  \"continued # Doe, Jane\nother: 1 # Roe, Richard\n", []int{2, 3}},
		{"block indicator on a plain continuation", "key: plain\n  > continued # Doe, Jane\nother: x\n  | y # Roe, Richard\n", []int{2, 4}},
		{"continuation after a blank line", "key: plain\n\n  'continued # Doe, Jane\n", []int{3}},
		{"continuation of a value on its own line", "key:\n  plain\n  'continued # Doe, Jane\nb: 1 # Roe, Richard\n", []int{3, 4}},
		{"continuation of a sequence entry", "- plain\n  'continued # Doe, Jane\n- 'x # not a comment'\n", []int{2}},
		{"continuation of a document scalar", "plain\n'continued # Doe, Jane\n", []int{2}},
		{"continuation in a flow collection", "a: [x\n  'y # Doe, Jane\n  ]\n", []int{2}},
		{"a sibling key is not a continuation", "a: x\nb: 'y # z'\nc: 1 # Doe, Jane\n", []int{3}},
		{"a key in a sequence item is not a continuation", "- a: x\n  b: 'y # z'\n  c: 1 # Doe, Jane\n", []int{3}},
		{"a comment ends a plain scalar", "a: x # Doe, Jane\nb: 'y # z'\n", []int{1}},
		{"a quoted value after a key still opens", "a: plain\nb: 'x # z'\nc: |\n  # z\nd: 1 # Doe, Jane\n", []int{5}},
		{"tagged and anchored values still open", "a: !t 'x # z'\nb: &n \"y # z\"\nc: 1 # Doe, Jane\n", []int{3}},
		{"a document marker ends a plain scalar", "plain\n--- 'x # z'\n# Doe, Jane\n", []int{3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n yaml.Node
			if err := yaml.Unmarshal([]byte(tt.src), &n); err != nil {
				t.Fatal("the test source does not parse")
			}
			if got := lines(comments(tt.src)); !slices.Equal(got, tt.lines) {
				t.Errorf("comment lines = %v, want %v", got, tt.lines)
			}
		})
	}
}

// The parser's comments are merged with the scanner's by text, so a
// comment either finds is judged once.
func TestAllCommentsMergesByText(t *testing.T) {
	src := "# Doe, Jane\na: 1 # Doe, Jane\nb: 2 # Roe, Richard\n"
	n, err := parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := allComments(src, n)
	var texts []string
	for _, c := range got {
		texts = append(texts, c.text)
	}
	want := []string{"Doe, Jane", "Doe, Jane", "Roe, Richard"}
	if !slices.Equal(texts, want) {
		t.Errorf("comments = %d %v, want %d", len(texts), lines(got), len(want))
	}
}

// A comment the scanner misses is still found through the parser.
func TestNodeCommentsCoverTheScanner(t *testing.T) {
	src := "# a\nx: 1 # b\n\n# c\ny:\n  - z # d\n"
	n, err := parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, c := range nodeComments(n) {
		texts = append(texts, c.text)
	}
	slices.Sort(texts)
	if want := []string{"a", "b", "c", "d"}; !slices.Equal(texts, want) {
		t.Errorf("node comments = %q, want %q", texts, want)
	}
}
