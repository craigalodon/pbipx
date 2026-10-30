package gate

import "testing"

func TestGitBashDrive(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"/c", `C:\`, true},
		{"/c/", `C:\`, true},
		{"/c/Users/x", `C:\Users\x`, true},
		{"/D/work/repo/", `D:\work\repo\`, true},
		{"/tmp/x", "", false},
		{"/", "", false},
		{"c/x", "", false},
		{`\c\x`, "", false},
		{"/1/x", "", false},
		{"C:/x", "", false},
	} {
		got, ok := gitBashDrive(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("gitBashDrive(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
