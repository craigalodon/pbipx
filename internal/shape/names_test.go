package shape

import "testing"

func TestObjectName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"student", "student"},
		{"Student Count", "Student Count"},
		{"course catalog", "course catalog"},
		{"Doe, Jane", Withheld},
		{"jane.doe@example.com", Withheld},
		{"Calls to 212-555-0142", Withheld},
		{"Enrolled 2105-03-14", Withheld},
		{"", Withheld},
		{"tab\there", Withheld},
	}
	for _, c := range cases {
		if got := ObjectName(c.name); got != c.want {
			t.Errorf("ObjectName(case %d) = %q, want %q", len(c.name), got, c.want)
		}
	}
}

func TestIdentifierPart(t *testing.T) {
	cases := []struct{ part, want string }{
		{"dbo", "dbo"},
		{"student_2105", "student_2105"},
		{"_staging", "_staging"},
		{"2105", NotIdentifier},
		{"", NotIdentifier},
		{"school-db", NotIdentifier},
		{"roster 2105", NotIdentifier},
		{"Doe, Jane", NotIdentifier},
		{"contoso.example.cloud", NotIdentifier},
		{"élève", NotIdentifier},
	}
	for _, c := range cases {
		if got := IdentifierPart(c.part); got != c.want {
			t.Errorf("IdentifierPart(case of length %d) = %q, want %q", len(c.part), got, c.want)
		}
	}
}
