package corpus

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"strings"
)

// sentinelForms are the spellings of a sentinel that count as the sentinel:
// as written, and with each apostrophe doubled, as a quoted literal in SQL,
// M or a PBIR string stores it.
func sentinelForms(s string) []string {
	forms := []string{s}
	if strings.Contains(s, "'") {
		forms = append(forms, strings.ReplaceAll(s, "'", "''"))
	}
	return forms
}

func containsAny(data []byte, forms []string) bool {
	for _, f := range forms {
		if bytes.Contains(data, []byte(f)) {
			return true
		}
	}
	return false
}

// enterDataBlob matches the base64 argument of Binary.FromText, which holds
// an "Enter data" table in TMDL.
var enterDataBlob = regexp.MustCompile(`Binary\.FromText\(\s*"([A-Za-z0-9+/=]*)"`)

// maxBlob bounds a decompressed Enter data blob.
const maxBlob = 16 << 20

// planted reports whether a fixture holds the sentinel: in any of its forms,
// and, in a .tmdl file, inside an Enter data blob, raw-deflated or plain.
// rel is the file's path, empty for stdin and args.
func planted(data []byte, rel, sentinel string) bool {
	forms := sentinelForms(sentinel)
	if containsAny(data, forms) {
		return true
	}
	if path.Ext(rel) != ".tmdl" {
		return false
	}
	for _, m := range enterDataBlob.FindAllSubmatch(data, -1) {
		raw, err := base64.StdEncoding.DecodeString(string(m[1]))
		if err != nil {
			continue
		}
		if containsAny(raw, forms) {
			return true
		}
		inflated, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(raw)), maxBlob))
		if err == nil && containsAny(inflated, forms) {
			return true
		}
	}
	return false
}

// containsSentinel reports whether output holds the sentinel in any of its
// forms, as written or as the body of a JSON string, escaped either way
// json.Marshal can escape it.
func containsSentinel(data []byte, s string) bool {
	for _, form := range sentinelForms(s) {
		if bytes.Contains(data, []byte(form)) {
			return true
		}
		for _, html := range []bool{true, false} {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(html)
			if enc.Encode(form) != nil {
				continue
			}
			body := bytes.TrimSuffix(bytes.TrimSpace(buf.Bytes()), []byte(`"`))
			body = bytes.TrimPrefix(body, []byte(`"`))
			if bytes.Contains(data, body) {
				return true
			}
		}
	}
	return false
}
