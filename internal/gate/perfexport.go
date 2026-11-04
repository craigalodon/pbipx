package gate

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"unicode/utf16"
)

// A Power BI Performance Analyzer export holds the text of the queries a
// report ran and the titles of its visuals, which can hold values. No
// scanner vets it, and one under a protected root that the scan scope
// catches would go to the report scanner, which may call it clean. So the
// gate denies a Read of a file that has its shape (behavior.md, "Files and
// the default deny"; principle 8: the deny comes before the capability).
// The command that will summarize one safely is `pbipx perf` (docs/design/perf.md).
//
// The shape is recognized, not the name, so a renamed export is caught, and
// the check opens only a bounded head of the file. Anything it cannot tell
// falls back to the existing rules: a file that is not read, not a JSON
// object, truncated before the shape decides, or simply different.

// The shape of an export, in one place. The field names are from memory of
// the export's general shape: THEY NEED CONFIRMING AGAINST A REAL EXPORT
// (schema only: names, never values). Until then the check is deliberately
// broad about what it calls an export, within one object holding a list of
// named, timed events.
const (
	// perfEventsKey is the top-level key of the event list.
	perfEventsKey = "events"
	// perfVersionKey is the top-level key of the format version.
	perfVersionKey = "version"
	// perfEventNameKey is the key every event carries.
	perfEventNameKey = "name"
)

// perfEventKeys are the keys that make a named object an event: its timing,
// its metrics and its place in the tree of events. An event needs the name
// and one of these.
var perfEventKeys = []string{"start", "end", "metrics", "id", "parentId"}

// perfHeadLimit is how much of a file the check reads. A real export puts the
// version and the first event at the start; a file whose shape is not
// decided in this much falls back to the existing rules, so a large lock file
// costs one bounded read and is not denied.
const perfHeadLimit = 1 << 20

// perfExport reports whether the file at path (an absolute path to a regular
// file) is a Performance Analyzer export. A file that cannot be opened or
// read is not: the existing rules decide it, as before.
func perfExport(path string) bool {
	f, err := os.Open(path) //nolint:gosec // G304: the path is the one the agent is about to read
	if err != nil {
		return false
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, perfHeadLimit))
	if err != nil {
		return false
	}
	return perfExportShape(head)
}

// perfExportShape reports whether head, the start of a file, begins a
// Performance Analyzer export: one JSON object with an event list of named,
// timed objects, or an empty one beside a version. It never returns an error
// or any of head: a parse that cannot tell is false.
func perfExportShape(head []byte) bool {
	head = decodeHead(head)
	head = bytes.TrimLeft(head, " \t\r\n")
	if len(head) == 0 || head[0] != '{' {
		return false // cheap exit for every file that is not an object
	}
	dec := json.NewDecoder(bytes.NewReader(head))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	version, emptyEvents := false, false
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return false
		}
		switch key {
		case perfVersionKey:
			version = true
		case perfEventsKey:
			switch perfEvents(dec) {
			case perfYes:
				return true
			case perfEmpty:
				emptyEvents = true
			default:
				return false
			}
			continue
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return emptyEvents && version
}

// What perfEvents found in the event list.
type perfVerdict int

const (
	perfNo    perfVerdict = iota // not a list of events, or cannot be told
	perfEmpty                    // an empty list
	perfYes                      // a list whose first element is an event
)

// perfEvents reads the value of the events key: the decoder is just past the
// key.
func perfEvents(dec *json.Decoder) perfVerdict {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return perfNo
	}
	if !dec.More() {
		if _, err := dec.Token(); err != nil { // the closing bracket
			return perfNo
		}
		return perfEmpty
	}
	// Read the first element's keys as they come, so a large value in it
	// does not matter once its name and one more key are seen.
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return perfNo
	}
	named, timed := false, false
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return perfNo
		}
		named = named || key == perfEventNameKey
		timed = timed || slices.Contains(perfEventKeys, key)
		if named && timed {
			return perfYes
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return perfNo
		}
	}
	return perfNo
}

// decodeHead drops a UTF-8 byte order mark and turns UTF-16 (marked by its
// byte order mark) into UTF-8, so the shape check sees the same text. Other
// input is returned as it is.
func decodeHead(b []byte) []byte {
	switch {
	case bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}):
		return b[3:]
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}), bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		little := b[0] == 0xff
		b = b[2:]
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			if little {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i+1])|uint16(b[i])<<8)
			}
		}
		return []byte(string(utf16.Decode(u)))
	}
	return b
}

// hintPerf ends a refusal of an export. `pbipx perf` is the summary that
// prints counts and durations only (docs/design/perf.md).
const hintPerf = "Ask a person to summarize it with `pbipx perf` at a terminal, or leave it out of the call."
