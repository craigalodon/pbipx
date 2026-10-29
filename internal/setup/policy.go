package setup

import (
	"strings"
)

// PlaceholderRoot is the protected root a policy names when no PBIP project
// was found.
const PlaceholderRoot = "pbip"

// Image types allowed unscanned under StaticResources.
var imageExts = []string{"png", "jpg", "jpeg", "gif"}

// Policy is the commented starting policy for layout l (ADR-0004). With no
// roots it protects PlaceholderRoot and says so.
func Policy(l Layout) []byte {
	roots := l.Roots
	placeholder := len(roots) == 0
	if placeholder {
		roots = []string{PlaceholderRoot}
	}
	perRoot := func(suffixes ...string) []string {
		var out []string
		for _, r := range roots {
			for _, s := range suffixes {
				out = append(out, r+"/"+s)
			}
		}
		return out
	}
	report := perRoot("**/*.json", "**/*.pbir", "**/*.pbism", "**/*.pbip", "**/.platform")
	if l.TopLevelPBIP {
		report = append(report, "*.pbip")
	}
	var images []string
	for _, ext := range imageExts {
		images = append(images, perRoot("**/StaticResources/**/*."+ext)...)
	}

	var b strings.Builder
	b.WriteString(header)
	if placeholder {
		b.WriteString(placeholderNote)
	} else {
		b.WriteString("#\n# Found by `pbipx init`: the directories that hold the PBIP folders.\n")
	}
	b.WriteString("protected_roots = " + array(roots) + "\n")
	b.WriteString(scanIntro)
	b.WriteString("report  = " + array(report) + "\n")
	b.WriteString("model   = " + array(perRoot("**/*.tmdl")) + "\n")
	b.WriteString(scanExamples)
	b.WriteString(filesSection)
	b.WriteString("unscanned_allow = " + array(images) + "\n")
	b.WriteString(rest)
	return []byte(b.String())
}

// array writes a TOML array of basic strings, on one line when short and
// one item per line otherwise. Items are writable paths and globs, which
// need no escapes.
func array(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = `"` + s + `"`
	}
	one := "[" + strings.Join(quoted, ", ") + "]"
	if len(one) <= 60 {
		return one
	}
	return "[\n  " + strings.Join(quoted, ",\n  ") + ",\n]"
}

const header = `# pbipx policy (ADR-0004), written by ` + "`pbipx init`" + `.
#
# Everything that decides a verdict is in this file. Review it before you
# rely on it, above all [columns], and approve each change at a terminal with
# ` + "`pbipx policy approve`" + ` (ADR-0006).
#
# Globs match repository-relative paths with forward slashes: ` + "`*`" + ` and ` + "`?`" + `
# within one segment, ` + "`[...]`" + ` a class, ` + "`**`" + ` any number of segments. Matching
# is case-sensitive.

version = 1

# Every file under a protected root is scanned, local state, an allowed
# image, or else denied to an agent as unvettable (principle 8).
`

const placeholderNote = `#
# PLACEHOLDER: ` + "`pbipx init`" + ` found no PBIP project (*.Report or *.SemanticModel
# folder, or .pbip file). Put your projects under this directory, or change
# it, and the globs below, to where they are.
`

const scanIntro = `
# What each scanner reads, per file kind.
[scan]
`

const scanExamples = `# Neighbouring files can be scanned too. Uncomment and adjust, for example:
# sql   = ["sql/**/*.sql"]
# yaml  = ["models/**/*.yml", "models/**/*.yaml"]
# prose = ["notes/**/*.md", "docs/**/*.md"]
# Removed from every kind. Desktop's .pbi folder is never scanned.
exclude = ["**/.pbi/**"]

`

const filesSection = `[files]
# Power BI Desktop's UI state: never read by an agent, skipped by tree scans.
local_state = ["**/.pbi/localSettings.json", "**/.pbi/editorSettings.json"]
# Opaque files allowed unscanned under a protected root: report images.
`

const rest = `
[columns]
# A generic starting list. Edit it for your data: add the columns of your
# own models that identify a person, and remove what does not apply. Names
# are bare column names, compared lowercased.
#
# Direct identifiers: a persisted value in one of these is a block.
block = [
  "full_name", "first_name", "middle_name", "last_name", "preferred_name",
  "email", "email_address", "phone", "phone_number", "mobile_number",
  "street_address", "address_line_1", "address_line_2",
  "date_of_birth", "birth_date",
  "ssn", "national_id", "passport_number",
  "person_id", "student_id", "employee_id", "customer_id",
]
# A regular expression (RE2) over the lowercased bare name; a match blocks.
block_pattern = "(^|_)(email|phone|ssn)(_|$)"
# Quasi-identifiers: a persisted value is a warning, which a reviewer can
# accept below or record in the baseline.
warn = [
  "birth_year", "age", "gender", "ethnicity", "postal_code", "zip_code",
  "cohort_year", "graduation_year", "section_number",
]

[shapes]
# Values that look like personal data, whatever their column. Each is on
# unless turned off here.
person_name = true
email       = true
ssn         = true
date        = true
phone       = true

[values]
# Literals never judged. Keep this short: each entry is a value the scan
# will not look at anywhere.
#
# A single-select slicer on a person column saves its first value without
# anyone clicking. Save it on a sentinel that names no one, such as a
# "(none selected)" row in the model, and list that sentinel here.
allow = []

# A lasting acceptance of one warn column under one path, for a persisted
# selection that is intended:
#
# [[accept]]
# column      = "enrollment.cohort_year"
# path        = "reports/Example.Report/definition"
# note        = "why this persisted selection is intended"
# reviewed_by = "a reviewer"
`
