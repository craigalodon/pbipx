package claudecode

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// maxLSPDepth bounds how deep lspPaths looks into an LSP call's input.
const maxLSPDepth = 32

// lspPaths are the paths an LSP call's input names: every string, at any
// depth, under a key whose name holds "path", "file" or "uri", case
// ignored. A file:// URI is decoded to its path; any other string is
// judged as written. A value under such a key that is a number
// or a boolean, a file URI that does not parse, or an input nested past
// maxLSPDepth is errEvent: the input is not documented, so what cannot be
// read denies.
func lspPaths(input map[string]json.RawMessage) ([]string, error) {
	var out []string
	var walk func(v any, named bool, depth int) error
	walk = func(v any, named bool, depth int) error {
		if depth > maxLSPDepth {
			return errEvent
		}
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := walk(x[k], named || lspKey(k), depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, c := range x {
				if err := walk(c, named, depth+1); err != nil {
					return err
				}
			}
		case string:
			if !named || x == "" {
				return nil
			}
			p, err := filePath(x)
			if err != nil {
				return err
			}
			out = append(out, p)
		case nil:
		default:
			if named {
				return errEvent
			}
		}
		return nil
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var v any
		if json.Unmarshal(input[k], &v) != nil {
			return nil, errEvent
		}
		if err := walk(v, lspKey(k), 1); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// lspKey reports whether an LSP input key names a path.
func lspKey(k string) bool {
	k = strings.ToLower(k)
	return strings.Contains(k, "path") || strings.Contains(k, "file") || strings.Contains(k, "uri")
}

// uriScheme is a URI's scheme as RFC 3986 spells one, two characters or
// more, so that a Windows drive ("C:") is not taken for one.
var uriScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]+:`)

// windowsDrivePath is a file URI's path on a Windows drive: "/C:/x".
var windowsDrivePath = regexp.MustCompile(`^/[A-Za-z]:`)

// filePath is s as a path: the decoded path of a file:// URI, a host other
// than localhost kept as a UNC prefix, and s itself otherwise.
func filePath(s string) (string, error) {
	m := uriScheme.FindString(s)
	if !strings.EqualFold(m, "file:") {
		// Not a file URI, or another scheme ("main.go:12" reads as one):
		// judged as written, as a path.
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Path == "" {
		return "", errEvent
	}
	p := u.Path
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "//" + u.Host + p, nil
	}
	if windowsDrivePath.MatchString(p) {
		p = p[1:]
	}
	return p, nil
}
