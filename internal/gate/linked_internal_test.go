package gate

import "testing"

func TestConfigWorktree(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"[core]\n\tbare = false\n\tworktree = /work/tree\n", "/work/tree"},
		{"[Core]\n\tWorkTree = \"../tree\"\n", "../tree"},
		{"[core]\n\tbare = true\n[remote \"origin\"]\n\tworktree = /not/core\n", ""},
		{"", ""},
	} {
		if got := configWorktree(tc.text); got != tc.want {
			t.Errorf("configWorktree(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
