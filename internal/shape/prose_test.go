package shape

import (
	"slices"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/secret"
)

var (
	name  = []finding.Reason{finding.PersonNameShape}
	email = []finding.Reason{finding.EmailShape}
	ssn   = []finding.Reason{finding.SSNShape}
	phone = []finding.Reason{finding.PhoneShape}
)

func TestProseLine(t *testing.T) {
	tests := []struct {
		why   string
		line  string
		allow []string
		want  []finding.Reason
	}{
		// The name standing alone: the whole line, after lead markers.
		{"a whole line", "Roe, Richard", nil, name},
		{"a whole line with surrounding spaces", "  Roe, Richard  ", nil, name},
		{"after a bullet", "- Doe, Jane", nil, name},
		{"after a star bullet", "* Doe, Jane", nil, name},
		{"after a list number", "12. Roe, Richard", nil, name},
		{"after a list number with a parenthesis", "3) Roe, Richard", nil, name},
		{"after a quote marker", "> Example, Pat", nil, name},
		{"after a quote marker without a space", ">Example, Pat", nil, name},
		{"after nested markers", "> - [x] Example, Pat", nil, name},
		{"after a heading", "## Roe, Richard", nil, name},
		{"with two more words", "Doe, Jane Ann Marie", nil, name},
		{"with two lowercase words", "- Doe, Jane and Pat", nil, name},
		{"with trailing punctuation", "- Doe, Jane.", nil, name},
		{"non-ASCII", "- Éxample, Pät", nil, name},
		{"titlecase digraph", "- ǅoe, Jane", nil, name},
		{"curly apostrophe in the last name", "- O’Doe, Jane", nil, name},

		// The name standing alone: a field.
		{"a pipe table cell", "| 7 | Doe, Jane | 9 |", nil, name},
		{"a pipe table cell with two more words", "| Doe, Jane Ann Marie | 9 |", nil, name},
		{"a tab-separated field", "row 7\tRoe, Richard\tlevel 2", nil, name},
		{"in double quotes", `The sample owner was "Doe, Jane" last time.`, nil, name},
		{"in backticks", "The sample owner was `Roe, Richard` last time.", nil, name},
		{"in curly double quotes", "The sample owner was “Example, Pat” last time.", nil, name},
		{"in curly single quotes", "The sample owner was ‘Example, Pat’ last time.", nil, name},
		{"in straight single quotes", "The sample owner was 'Example, Pat' last time.", nil, name},
		{"a doubled apostrophe, un-doubled", "WHERE owner = 'O''Doe, Jane'", nil, name},
		{"a quoted name with an apostrophe", "WHERE owner = 'O'Doe, Jane'", nil, name},

		// Noise: Word, Word that is not a name standing alone.
		{"inside a sentence", "The chart compares Fall, Spring and Summer side by side.", nil, nil},
		{"a wrapped line starting Word, Word", "Term, School and Grade in the summary view.", nil, nil},
		{"a line ending in a comma", "Enrollment is broken down by Region,", nil, nil},
		{"three more words in a cell", "| Totals, Region North South East | 12 |", nil, nil},
		{"three more words on a line", "- Totals, Region North South East", nil, nil},
		{"a comma ends a word: a list of three", "- Fall, Spring, Summer", nil, nil},
		{"a comma ends a word: in a cell", "| Fall, Spring, Summer |", nil, nil},
		{"trailing punctuation that is not a word character", "- Doe, Jane;", nil, nil},
		{"an extra word must start with a letter", "- Doe, Jane 9", nil, nil},
		{"accepted noise: Word, Word alone in a cell", "| Totals, Region |", nil, name},
		{"words may hold digits, dots, hyphens and apostrophes", "- Doe, Jane-Ann O'Roe Jr.", nil, name},
		{"contractions do not split", "Budget, Review don't match; it's a known gap.", nil, nil},
		{"curly contractions do not split", "Budget, Review don’t match; it’s a known gap.", nil, nil},
		{"a name after other text on the line", "Owner: Doe, Jane", nil, nil},
		{"lowercase initials", "- doe, jane", nil, nil},
		{"a heading marker without a space", "##Roe, Richard", nil, nil},
		{"a bullet without a space", "-Roe, Richard", nil, nil},
		{"no space after the comma", "- Doe,Jane", nil, nil},
		{"empty", "", nil, nil},
		{"markers only", "> - ", nil, nil},

		// Email, SSN and phone anywhere; no date.
		{"an email mid-sentence", "Questions go to jane.doe@example.com before the release.", nil, email},
		{"two emails, one line", "Copy jane.doe@example.com and richard.roe@example.org.", nil, email},
		{"an SSN mid-sentence", "The test row used 900-12-3456 as its id.", nil, ssn},
		{"a phone mid-sentence", "The desk is at 555-555-0142 during the term.", nil, phone},
		{"an ISO date", "The review is due 2101-03-15.", nil, nil},
		{"a US date", "The term ends 6/30/2101.", nil, nil},
		{"a version number", "Upgrade to 2.101.3 before 2101-03-15.", nil, nil},
		{"a short phone", "Extension 555-0142 is the desk.", nil, nil},
		{
			"every prose shape on one row, in reason order",
			"| Doe, Jane | jane.doe@example.com | 555-555-0142 | 900-12-3456 |",
			nil,
			[]finding.Reason{finding.PersonNameShape, finding.EmailShape, finding.SSNShape, finding.PhoneShape},
		},
		{"an email in a comment-looking line", "# Author: Doe, Jane <jane.doe@example.com>", nil, email},

		// The allow list.
		{"an allowlisted name and one more word", "- Example, Pat (placeholder)", []string{"Example, Pat"}, nil},
		{"an allowlisted name in a cell", "| Example, Pat | 9 |", []string{"Example, Pat"}, nil},
		{"an allowlisted name leaves another", "| Example, Pat | Doe, Jane |", []string{"Example, Pat"}, name},
		{"an allowlisted name quoted SQL-style", "WHERE owner = 'O''Example, Pat'", []string{"O'Example, Pat"}, nil},
		{"an allowlisted email", "Write to pat@example.net.", []string{"pat@example.net"}, nil},
		{"a removed literal leaves a space, not a join", "Write to jane.doe@example(none selected).com", []string{"(none selected)"}, nil},
		{"an empty allow entry is ignored", "- Doe, Jane", []string{""}, name},
		{"allow matches exactly, case included", "- Example, Pat", []string{"example, pat"}, name},
		{"the longer allow entry goes first", "- Example, Pat Doe", []string{"Example, Pat", "Example, Pat Doe"}, nil},
		{"an allowed word does not cut a name it starts", "- Allen, Jane", []string{"All"}, name},
		{"an allowed word does not cut a name it ends", "- Roe, Janet", []string{"et"}, name},
		{"an allowed name between punctuation is removed", "- (Example, Pat).", []string{"Example, Pat"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			if got := ProseLine(tt.line, tt.allow, Switches{}); !slices.Equal(got, tt.want) {
				t.Errorf("ProseLine() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProseLineSwitches(t *testing.T) {
	row := "| Doe, Jane | jane.doe@example.com | 555-555-0142 | 900-12-3456 |"
	tests := []struct {
		sw   Switches
		want []finding.Reason
	}{
		{Switches{PersonNameOff: true}, []finding.Reason{finding.EmailShape, finding.SSNShape, finding.PhoneShape}},
		{Switches{EmailOff: true}, []finding.Reason{finding.PersonNameShape, finding.SSNShape, finding.PhoneShape}},
		{Switches{SSNOff: true}, []finding.Reason{finding.PersonNameShape, finding.EmailShape, finding.PhoneShape}},
		{Switches{PhoneOff: true}, []finding.Reason{finding.PersonNameShape, finding.EmailShape, finding.SSNShape}},
		{Switches{PersonNameOff: true, EmailOff: true, SSNOff: true, PhoneOff: true}, nil},
	}
	for _, tt := range tests {
		if got := ProseLine(row, nil, tt.sw); !slices.Equal(got, tt.want) {
			t.Errorf("ProseLine(%+v) = %v, want %v", tt.sw, got, tt.want)
		}
	}
	if got := ProseLine("Due 2101-03-15.", nil, Switches{}); got != nil {
		t.Errorf("a date in prose = %v, want nothing", got)
	}
}

func TestProse(t *testing.T) {
	text := "\xef\xbb\xbfReviewers:\r\n" +
		"\r\n" +
		"- Doe, Jane\r\n" +
		"Copy jane.doe@example.com and richard.roe@example.org.\n" +
		"The summary page is clean now.\n" +
		"# Author: Roe, Richard <richard.roe@example.org>\n" +
		"Send the export to pat@example.net.\n"

	type want struct {
		line    int
		reasons []finding.Reason
		text    string
	}
	check := func(t *testing.T, got []Hit, wants []want) {
		t.Helper()
		if len(got) != len(wants) {
			t.Fatalf("got %d hits, want %d", len(got), len(wants))
		}
		for i, w := range wants {
			if got[i].Line != w.line || !slices.Equal(got[i].Reasons, w.reasons) {
				t.Errorf("hit %d: line %d %v, want line %d %v", i, got[i].Line, got[i].Reasons, w.line, w.reasons)
			}
			// The hit carries the line as written, which its silhouette shows
			// without revealing it.
			if secret.Shape(got[i].Text) != secret.Shape(secret.New(w.text)) {
				t.Errorf("hit %d does not carry its line", i)
			}
		}
	}

	t.Run("every line", func(t *testing.T) {
		check(t, Prose(text, ProseOptions{}), []want{
			{3, name, "- Doe, Jane"},
			{4, email, "Copy jane.doe@example.com and richard.roe@example.org."},
			{6, email, "# Author: Roe, Richard <richard.roe@example.org>"},
			{7, email, "Send the export to pat@example.net."},
		})
	})
	t.Run("comment lines are scanned with a scissors line set", func(t *testing.T) {
		check(t, Prose(text, ProseOptions{Scissors: ScissorsLine("#")}), []want{
			{3, name, "- Doe, Jane"},
			{4, email, "Copy jane.doe@example.com and richard.roe@example.org."},
			{6, email, "# Author: Roe, Richard <richard.roe@example.org>"},
			{7, email, "Send the export to pat@example.net."},
		})
	})
	t.Run("scanning stops at the scissors line", func(t *testing.T) {
		msg := "Subject jane.doe@example.com\n" + ScissorsLine("#") + "\r\n- Doe, Jane\npat@example.net\n"
		check(t, Prose(msg, ProseOptions{Scissors: ScissorsLine("#")}), []want{
			{1, email, "Subject jane.doe@example.com"},
		})
	})
	t.Run("a scissors line with another comment character does not stop", func(t *testing.T) {
		msg := ScissorsLine(";") + "\npat@example.net\n"
		check(t, Prose(msg, ProseOptions{Scissors: ScissorsLine("#")}), []want{
			{2, email, "pat@example.net"},
		})
	})
	t.Run("without the option the scissors line is prose", func(t *testing.T) {
		msg := ScissorsLine("#") + "\npat@example.net\n"
		check(t, Prose(msg, ProseOptions{}), []want{
			{2, email, "pat@example.net"},
		})
	})
	t.Run("allow list and switches", func(t *testing.T) {
		check(t, Prose(text, ProseOptions{
			Allow:    []string{"pat@example.net"},
			Switches: Switches{PersonNameOff: true},
		}), []want{
			{4, email, "Copy jane.doe@example.com and richard.roe@example.org."},
			{6, email, "# Author: Roe, Richard <richard.roe@example.org>"},
		})
	})
	t.Run("clean text", func(t *testing.T) {
		if got := Prose("Enrollment is broken down by Region,\nTerm, School and Grade in the view.\n", ProseOptions{}); got != nil {
			t.Errorf("got %d hits, want none", len(got))
		}
	})
}

func TestCleanIsWordBounded(t *testing.T) {
	tests := []struct {
		line  string
		allow []string
		want  string
	}{
		{"The slicer saves (none selected) as its value.", []string{"(none selected)"}, "The slicer saves   as its value."},
		{"(none selected)", []string{"(none selected)"}, " "},
		{"Allen, Jane", []string{"All"}, "Allen, Jane"},
		{"All, Allen, All", []string{"All"}, " , Allen,  "},
		{"Roe, Janet", []string{"et"}, "Roe, Janet"},
		{"x9(none selected)", []string{"(none selected)"}, "x9(none selected)"},
	}
	for _, tt := range tests {
		if got := clean(tt.line, tt.allow); got != tt.want {
			t.Errorf("clean(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
