package policy

import (
	"errors"
	"testing"
)

func TestCompileGlobErrors(t *testing.T) {
	tests := []struct {
		pattern string
		want    error
	}{
		{"", errGlobEmpty},
		{"/pbip/**", errGlobAbsolute},
		{"pbip//x.json", errGlobEmptySeg},
		{"pbip/", errGlobEmptySeg},
		{"pbip/a**/x.json", errGlobDoubleStar},
		{"pbip/**.json", errGlobDoubleStar},
		{"pbip/**/[.json", errGlobSyntax},
		{"pbip/[a-", errGlobSyntax},
		{"pbip/x[", errGlobSyntax},
		{"pbip/[]", errGlobSyntax},
		{`pbip/x\`, errGlobSyntax},
	}
	for _, tt := range tests {
		if _, err := compileGlob(tt.pattern); !errors.Is(err, tt.want) {
			t.Errorf("compileGlob(%q) = %v, want %v", tt.pattern, err, tt.want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"pbip/**/*.json", "pbip/a.json", true},
		{"pbip/**/*.json", "pbip/A.Report/definition/pages/0000/page.json", true},
		{"pbip/**/*.json", "pbip/a.tmdl", false},
		{"pbip/**/*.json", "other/a.json", false},
		{"pbip/**/*.json", "pbip", false},
		{"pbip/*.json", "pbip/a/b.json", false},
		{"pbip/*.json", "pbip/.json", true},
		{"pbip/?.json", "pbip/a.json", true},
		{"pbip/?.json", "pbip/ab.json", false},
		{"pbip/[ab].json", "pbip/b.json", true},
		{"pbip/[ab].json", "pbip/c.json", false},
		{"pbip/[^ab].json", "pbip/c.json", true},
		{"pbip/[a-c]x", "pbip/bx", true},
		{`pbip/\[x`, "pbip/[x", true},
		{"pbip/**/*.json", "PBIP/a.json", false},
		{"pbip/*.JSON", "pbip/a.json", false},
		{"**/.pbi/**", "pbip/A.Report/.pbi/localSettings.json", true},
		{"**/.pbi/**", ".pbi/x", true},
		{"**/.pbi/**", "pbip/A.Report/pbi/x", false},
		{"**/.pbi/localSettings.json", "pbip/A/.pbi/localSettings.json", true},
		{"**", "anything/at/all", true},
		{"**/**/x", "x", true},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/1/2/b/3/c", true},
		{"a/**/b/**/c", "a/1/2/c", false},
		{"pbip/**/StaticResources/**/*.png", "pbip/A.Report/StaticResources/RegisteredResources/logo.png", true},
		{"pbip/**/StaticResources/**/*.png", "pbip/A.Report/StaticResources/logo.jpg", false},
		{"pbip/*", "pbip/a/b", false},
	}
	for _, tt := range tests {
		g, err := compileGlob(tt.pattern)
		if err != nil {
			t.Fatalf("compileGlob(%q): %v", tt.pattern, err)
		}
		if got := g.match(tt.path); got != tt.want {
			t.Errorf("%q matching %q = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestGlobManyDoubleStarsStayFast(t *testing.T) {
	g, err := compileGlob("a/**/a/**/a/**/a/**/a/**/a/**/b")
	if err != nil {
		t.Fatal(err)
	}
	path := "a"
	for range 200 {
		path += "/a"
	}
	if g.match(path) {
		t.Error("matched a path with no final b")
	}
}

func TestGlobCoversUnder(t *testing.T) {
	tests := []struct {
		pattern, dir string
		want         bool
	}{
		{"pbip/**/*.json", "pbip", true},
		{"pbip/**/*.json", "pbip/A.Report", true},
		{"pbip/**/*.json", "warehouse", false},
		{"**/*.sql", "warehouse", true},
		{"*/x.json", "pbip", true},
		{"p*/x.json", "pbip", true},
		{"q*/x.json", "pbip", false},
		{"pbip", "pbip", false},
		{"pbip/x.json", "pbip", true},
		{"pbip/x.json", "pbip/sub", false},
		{"pbip/**", "pbip/sub/deeper", true},
		{"pbip/*/x.json", "pbip/sub", true},
		{"pbip/*/x.json", "pbip/sub/deeper", false},
	}
	for _, tt := range tests {
		g, err := compileGlob(tt.pattern)
		if err != nil {
			t.Fatalf("compileGlob(%q): %v", tt.pattern, err)
		}
		if got := g.coversUnder(tt.dir); got != tt.want {
			t.Errorf("%q covering %q = %v, want %v", tt.pattern, tt.dir, got, tt.want)
		}
	}
}

func TestEmptyGlobsMatchNothing(t *testing.T) {
	if (globs{}).match("pbip/a.json") || globs(nil).match("") {
		t.Error("an empty list matched")
	}
}
