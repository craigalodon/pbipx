package claudecode

import (
	"strings"
	"testing"
)

func TestQuoting(t *testing.T) {
	tests := []struct{ in, sh, ps string }{
		{"/usr/local/bin/pbipx", `'/usr/local/bin/pbipx'`, `'/usr/local/bin/pbipx'`},
		{"/a b/it's/$x`y`", `'/a b/it'\''s/$x` + "`y`'", `'/a b/it''s/$x` + "`y`'"},
		{`C:\Program Files\pbipx\pbipx.exe`, `'C:\Program Files\pbipx\pbipx.exe'`, `'C:\Program Files\pbipx\pbipx.exe'`},
		{"C:\\o\u2019brien\\pbipx.exe", "'C:\\o\u2019brien\\pbipx.exe'", "'C:\\o\u2019\u2019brien\\pbipx.exe'"},
	}
	for _, tt := range tests {
		if got := shQuote(tt.in); got != tt.sh {
			t.Errorf("shQuote(%q) = %q, want %q", tt.in, got, tt.sh)
		}
		if got := psQuote(tt.in); got != tt.ps {
			t.Errorf("psQuote(%q) = %q, want %q", tt.in, got, tt.ps)
		}
	}
}

func TestHookCommandForms(t *testing.T) {
	if got := HookCommand("/opt/pbipx", "linux"); !strings.HasPrefix(got, "set +e; '/opt/pbipx' gate --agent claude-code;") {
		t.Errorf("the POSIX command does not run the binary first")
	}
	if got := HookCommand(`C:\pbipx.exe`, "windows"); !strings.Contains(got, `& 'C:\pbipx.exe' gate --agent claude-code`) {
		t.Errorf("the PowerShell command does not run the binary")
	}
	if HookShell("windows") != "powershell" || HookShell("darwin") != "" || HookShell("linux") != "" {
		t.Error("HookShell")
	}
}
