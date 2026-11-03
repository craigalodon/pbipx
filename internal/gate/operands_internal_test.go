package gate

import "testing"

// TestNeverReadsOperandsTable: each program in the table is one that does
// not read its operands as files: none is a reader, a search, an
// interpreter or a shell, and the programs that read a file they are given
// to print or copy it stay out.
func TestNeverReadsOperandsTable(t *testing.T) {
	for name := range neverReadsOperands {
		if readers[name] {
			t.Errorf("%s is a reader", name)
		}
		if _, ok := searchers[name]; ok {
			t.Errorf("%s is a search", name)
		}
		if isInterpreter(name) {
			t.Errorf("%s is an interpreter", name)
		}
		if shells[name] {
			t.Errorf("%s is a shell", name)
		}
	}
	// Content printers and copiers outside the table.
	for _, name := range []string{
		"cat", "shuf", "expand", "unexpand", "ptx", "nl", "od", "fmt", "tac", "head", "tail", "cp", "tar",
		"jq", "python3", "perl", "node", "ruby", "curl", "wget", "tee", "sort", "uniq", "xxd", "strings",
		"unzip", "zip", "rsync", "sed", "awk", "grep", "rg", "make", "go", "npm", "pnpm", "yarn", "cargo", "rustup",
		"pip", "pip3", "pipx", "gem", "apt", "apt-get", "apt-cache", "brew", "dnf", "yum", "pacman", "apk", "snap",
		"man", "info", "help", "apropos", "whatis", "gh", "date",
	} {
		if neverReadsOperands[name] {
			t.Errorf("%s reads its operands and must not be in the table", name)
		}
		if !readsOperands(name) && !shells[name] {
			t.Errorf("%s: readsOperands is false", name)
		}
	}
}
