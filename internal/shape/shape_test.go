package shape

import (
	"slices"
	"testing"

	"github.com/craigalodon/pbipx/internal/finding"
)

// Every value here is synthetic (CONTRIBUTING.md#synthetic-data).

type shapeCase struct {
	in   string
	want bool
	why  string
}

func runShape(t *testing.T, name string, f func(string) bool, cases []shapeCase) {
	t.Helper()
	for _, c := range cases {
		if got := f(c.in); got != c.want {
			t.Errorf("%s: %s: got %v, want %v", name, c.why, got, c.want)
		}
	}
}

func TestPersonName(t *testing.T) {
	runShape(t, "PersonName", PersonName, []shapeCase{
		{"Doe, Jane", true, "plain Last, First"},
		{"Roe, Richard Q.", true, "more after the first name"},
		{"Doe,Jane", false, "no whitespace after the comma"},
		{"Doe,  Jane", true, "several spaces after the comma"},
		{"Doe, Jane", true, "a no-break space after the comma"},
		{"  Doe, Jane", true, "leading whitespace"},
		{"Doe-Roe, Jane", true, "hyphenated last name"},
		{"O'Doe, Jane", true, "straight apostrophe in the last name"},
		{"O’Doe, Jane", true, "curly apostrophe in the last name"},
		{"O''Doe, Jane", false, "a doubled apostrophe is not a join; callers un-double first"},
		{"Doe-, Jane", false, "a join must be followed by a letter run"},
		{"Éxample, Pät", true, "non-ASCII initials"},
		{"Ωmega, Δelta", true, "non-Latin uppercase initials"},
		{"ǅoe, Jane", true, "titlecase digraph initial"},
		{"Doe, ǈane", true, "titlecase digraph first initial"},
		{"Exámple, Pat", true, "a combining mark inside the last name"},
		{"doe, Jane", false, "lowercase last initial"},
		{"Doe, jane", false, "lowercase first initial"},
		{"Doe, 9", false, "a digit after the comma"},
		{"Doe Roe, Jane", false, "a space in the last name"},
		{"The owner is Doe, Jane", false, "anchored: not at the start"},
		{"Totals, Region", true, "any Word, Word at the start has the shape"},
		{"", false, "empty"},
	})
}

func TestEmail(t *testing.T) {
	runShape(t, "Email", Email, []shapeCase{
		{"jane.doe@example.com", true, "plain address"},
		{"Write to richard+notes@mail.example.org today", true, "inside a sentence, with a plus tag"},
		{"<pat_example@example.net>", true, "in angle brackets"},
		{"jane.doe@example", false, "no top-level domain"},
		{"@example.com", false, "no local part"},
		{"HEAD@{1}", false, "a git reflog selector"},
		{"tool@1.2.3", false, "a pinned version"},
		{"jane.doe at example.com", false, "spelled out"},
	})
}

func TestSSN(t *testing.T) {
	runShape(t, "SSN", SSN, []shapeCase{
		{"900-12-3456", true, "whole literal"},
		{"used 666-01-2345 as its id", true, "inside a sentence"},
		{"(000-12-3456)", true, "in parentheses"},
		{"9900-12-3456", false, "a longer first group"},
		{"900-12-34567", false, "a longer last group"},
		{"A900-12-3456", false, "joined to a letter"},
		{"900-123-4567", false, "phone grouping"},
		{"900 12 3456", false, "spaces, not hyphens"},
		{"2101-12-31", false, "an ISO date"},
	})
}

func TestDate(t *testing.T) {
	runShape(t, "Date", Date, []shapeCase{
		{"2101-03-15", true, "ISO, 2100s"},
		{"born 1999-12-31.", true, "ISO, 1900s, in a sentence"},
		{"2050-01-01", true, "ISO, 2000s"},
		{"6/30/2101", true, "US, 2100s"},
		{"06/03/2199", true, "US with leading zeros"},
		{"1/1/1900", true, "US, 1900s"},
		{"2201-03-15", false, "year 22xx"},
		{"1899-12-31", false, "year 18xx"},
		{"2101-13-01", false, "month 13"},
		{"2101-03-32", false, "day 32"},
		{"13/01/2101", false, "US month 13"},
		{"6/30/21", false, "two-digit year"},
		{"12101-03-15", false, "joined to a digit"},
		{"2101-03-150", false, "a longer day"},
		{"v2101-03-15", false, "joined to a letter"},
		{"2101/03/15", false, "slashes in ISO order"},
		{"3/4", false, "a fraction"},
	})
}

func TestPhone(t *testing.T) {
	runShape(t, "Phone", Phone, []shapeCase{
		{"555-555-0142", true, "hyphens"},
		{"555.555.0142", true, "dots"},
		{"555 555 0142", true, "spaces"},
		{"(555) 555-0142", true, "parenthesized area code"},
		{"(555)555-0142", true, "parenthesized area code, no space"},
		{"+1 555-555-0142", true, "with +1"},
		{"+1-555-555-0142", true, "with +1 and a hyphen"},
		{"call 555-555-0142.", true, "in a sentence"},
		{"555-0142", false, "no area code"},
		{"5555550142", false, "no separators"},
		{"1555-555-0142", false, "joined to a digit"},
		{"555-555-01425", false, "a longer last group"},
		{"x555-555-0142", false, "joined to a letter"},
		{"555-55-0142", false, "short middle group"},
		{"900-12-3456", false, "an SSN"},
		{"2101-03-15", false, "a date"},
		{"10.0.0.1", false, "an address"},
		{"1,234,567,890", false, "a large number"},
	})
}

func TestLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		sw   Switches
		want []finding.Reason
	}{
		{"nothing", "Summary page", Switches{}, nil},
		{"name", "Doe, Jane", Switches{}, []finding.Reason{finding.PersonNameShape}},
		{
			"every shape, in reason order",
			"Doe, Jane 555-555-0142 2101-03-15 900-12-3456 jane.doe@example.com",
			Switches{},
			[]finding.Reason{finding.PersonNameShape, finding.EmailShape, finding.SSNShape, finding.DateShape, finding.PhoneShape},
		},
		{
			"every shape off",
			"Doe, Jane 555-555-0142 2101-03-15 900-12-3456 jane.doe@example.com",
			Switches{PersonNameOff: true, EmailOff: true, SSNOff: true, DateOff: true, PhoneOff: true},
			nil,
		},
		{"name off", "Doe, Jane", Switches{PersonNameOff: true}, nil},
		{"email off", "jane.doe@example.com", Switches{EmailOff: true}, nil},
		{"ssn off", "900-12-3456", Switches{SSNOff: true}, nil},
		{"date off", "2101-03-15", Switches{DateOff: true}, nil},
		{"phone off", "555-555-0142", Switches{PhoneOff: true}, nil},
		{
			"one off leaves the others",
			"Doe, Jane 2101-03-15",
			Switches{PersonNameOff: true},
			[]finding.Reason{finding.DateShape},
		},
		{"a date in a literal counts", "6/30/2101", Switches{}, []finding.Reason{finding.DateShape}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Literal(tt.in, tt.sw); !slices.Equal(got, tt.want) {
				t.Errorf("Literal() = %v, want %v", got, tt.want)
			}
		})
	}
}
