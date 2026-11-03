package gate

import (
	"maps"
	"slices"
	"testing"
)

// TestProgramTablesFold: every table the gate looks a program up in by
// its name must find each of its entries under the name progName gives on
// a file system that folds case (Windows, macOS), where RG.EXE is rg. An
// entry spelled with a capital is never found there.
func TestProgramTablesFold(t *testing.T) {
	defer SetFoldCase(true)()
	tables := map[string][]string{
		"wrappers":          slices.Collect(maps.Keys(wrappers)),
		"inert":             slices.Collect(maps.Keys(inert)),
		"shells":            slices.Collect(maps.Keys(shells)),
		"interpreters":      slices.Collect(maps.Keys(interpreters)),
		"awks":              slices.Collect(maps.Keys(awks)),
		"runners":           slices.Collect(maps.Keys(runners)),
		"detachers":         slices.Collect(maps.Keys(detachers)),
		"redefiners":        slices.Collect(maps.Keys(redefiners)),
		"silent":            slices.Collect(maps.Keys(silent)),
		"pipeFilters":       slices.Collect(maps.Keys(pipeFilters)),
		"copiers":           slices.Collect(maps.Keys(copiers)),
		"readers":           slices.Collect(maps.Keys(readers)),
		"readOnlyWrappers":  slices.Collect(maps.Keys(readOnlyWrappers)),
		"readOnlyPrograms":  slices.Collect(maps.Keys(readOnlyPrograms)),
		"searchers":         slices.Collect(maps.Keys(searchers)),
		"gitTreeChangers":   slices.Collect(maps.Keys(gitTreeChangers)),
		"psRunners":         slices.Collect(maps.Keys(psRunners)),
		"psInert":           slices.Collect(maps.Keys(psInert)),
		"stdinScripts":      stdinScripts,
		"interpreter names": {"python3.14", "PYTHON3", "Rscript", "R", "Node22", "nodejs"},
	}
	for name, keys := range tables {
		for _, k := range keys {
			folded := progName(k)
			switch name {
			case "interpreter names":
				if _, ok := interpreters[interpreterFamily(folded)]; !ok {
					t.Errorf("%s: %q is not found as %q", name, k, folded)
				}
			case "stdinScripts":
				// Paths, not program names.
			default:
				if !slices.Contains(keys, folded) {
					t.Errorf("%s: %q is not found as %q on a file system that folds case", name, k, folded)
				}
			}
		}
	}
}
