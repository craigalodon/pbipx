package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// Readers other than the Read tool (behavior.md, "Files and the default
// deny", Performance Analyzer exports): a file a reading or searching
// command is given, or an input redirection names, is shape-checked as a
// Read is; a tree scan applies the same check to the JSON it reads.

func bash(name, cmd, reason string) decideCase {
	return decideCase{name, gate.Event{Tool: gate.Shell, Command: cmd}, reason}
}

func ps(name, cmd, reason string) decideCase {
	return decideCase{name, gate.Event{Tool: gate.PowerShell, Command: cmd}, reason}
}

func ordinaryAndExport() map[string]string {
	files := cleanFiles()
	files[exportOutside] = perfExport
	files["analysis/notes.txt"] = "nothing here\n"
	files["package.json"] = `{"name":"example","version":"1.0.0"}`
	files["tsconfig.json"] = `{"compilerOptions":{"strict":true}}`
	return files
}

func TestShellReadersOfAnExportDeny(t *testing.T) {
	root := newRepo(t, ordinaryAndExport())
	const pe = gate.ReasonPerfExport
	run(t, root, []decideCase{
		bash("cat", "cat analysis/trace.json", pe),
		bash("head", "head -n 5 analysis/trace.json", pe),
		bash("tail", "tail analysis/trace.json", pe),
		bash("jq", "jq . analysis/trace.json", pe),
		bash("jq filter", "jq '.events[]' analysis/trace.json", pe),
		bash("sed", "sed -n 1,5p analysis/trace.json", pe),
		bash("grep", "grep x analysis/trace.json", pe),
		bash("awk", "awk '{print}' analysis/trace.json", pe),
		bash("input redirection", "wc -c < analysis/trace.json", pe),
		bash("absolute", "cat "+bashPath(root)+"/analysis/trace.json", pe),
		bash("after cd", "cd analysis && cat trace.json", pe),
		bash("in a pipeline", "cat analysis/trace.json | head -n 2", pe),
		bash("with a glob", "cat analysis/tr*.json", pe),
		bash("an unknown program is taken to read its operands", "somereader analysis/trace.json", pe),
	})
	run(t, root, []decideCase{
		bash("cat package.json", "cat package.json", ""),
		bash("jq tsconfig", "jq . tsconfig.json", ""),
		bash("grep in package.json", "grep name package.json", ""),
		bash("a text file beside the export", "cat analysis/notes.txt", ""),
		bash("naming the export without reading it", "echo analysis/trace.json", ""),
		bash("listing the directory", "ls analysis", ""),
		bash("a missing file", "cat analysis/missing.json", ""),
	})
}

func TestShellReadersOfAnExportUnderARoot(t *testing.T) {
	files := cleanFiles()
	files[exportUnderRoot] = perfExport
	files["package.json"] = `{"name":"example"}`
	root := newRepo(t, files)
	const pe = gate.ReasonPerfExport
	run(t, root, []decideCase{
		bash("cat the file", "cat "+exportUnderRoot, pe),
		bash("grep -r over the root", "grep -rn x pbip", pe),
		bash("grep -r over the working directory", "grep -rn x .", pe),
		bash("cat package.json", "cat package.json", ""),
		{"Grep over the root", gate.Event{Tool: gate.Search, Path: "pbip"}, pe},
		{"Grep over the working directory", gate.Event{Tool: gate.Search}, pe},
	})
}

// A recursive search whose paths hold no root is not a tree scan: it stays
// as today (a limitation recorded in behavior.md).
func TestRecursiveSearchWithNoRootStaysAsToday(t *testing.T) {
	root := newRepo(t, ordinaryAndExport())
	run(t, root, []decideCase{
		{"Grep over a directory with no root", gate.Event{Tool: gate.Search, Path: "analysis"}, ""},
		bash("grep -r over a directory with no root", "grep -rn x analysis", ""),
	})
}

func TestPowerShellReadersOfAnExportDeny(t *testing.T) {
	root := newRepo(t, map[string]string{
		"README.md":         "An example repository.\n",
		reportJSON:          `{"layoutOptimization": "None"}`,
		exportOutside:       perfExport,
		"package.json":      `{"name":"example"}`,
		"analysis/note.txt": "x\n",
	})
	const pe = gate.ReasonPerfExport
	run(t, root, []decideCase{
		ps("Get-Content", "Get-Content analysis/trace.json", pe),
		ps("alias with a backslash path", `gc ".\analysis\trace.json"`, pe),
		ps("piped to ConvertFrom-Json", "Get-Content -Raw analysis/trace.json | ConvertFrom-Json", pe),
		ps("package.json", "Get-Content package.json", ""),
		ps("a text file", "Get-Content analysis/note.txt", ""),
	})
}
