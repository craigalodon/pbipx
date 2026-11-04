package gate

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"unicode/utf16"

	"github.com/craigalodon/pbipx/internal/scan"
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
// (schema only: names, never values). Common keys (`id`, `start`, `end`)
// are not enough, since calendars and logs have them: the shape is a
// top-level version, before an event list whose first event has a name and
// a key that is distinctive of Performance Analyzer.
const (
	// perfEventsKey is the top-level key of the event list.
	perfEventsKey = "events"
	// perfVersionKey is the top-level key of the format version. It must
	// come before the event list.
	perfVersionKey = "version"
	// perfEventNameKey is the key every event carries.
	perfEventNameKey = "name"
)

// perfEventKeys are the keys that, with the name, make an object an event of
// an export: its metrics and its parent in the tree of events.
var perfEventKeys = []string{"metrics", "parentId"}

// The cost of the check is bounded (principle 7: a hook that runs too long
// lets the call proceed). perfHeadLimit is how much of a file is read: a real
// export puts the version and the first event at the start, and a file whose
// shape is not decided in this much falls back to the existing rules.
// A command naming files is checked within a budget: past perfMaxFiles files
// opened, or perfMaxBytes bytes read, in one decision, the gate stops and
// denies with scan-error, since it can no longer say the rest are not exports.
const (
	perfHeadLimit = 64 << 10
	perfMaxFiles  = 512
	perfMaxBytes  = 16 << 20
)

// perfBudget is what one decision has spent on shape checks.
type perfBudget struct {
	files, bytes int
	over         bool
}

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
// Performance Analyzer export: one JSON object with a version, then an event
// list whose first element has a name and a distinctive key (perfEventKeys).
// It never returns an error or any of head: a parse that cannot tell is
// false.
func perfExportShape(head []byte) bool {
	head = head[:min(len(head), perfHeadLimit)]
	head = decodeHead(head)
	head = bytes.TrimLeft(head, " \t\r\n")
	if len(head) == 0 || head[0] != '{' {
		return false // cheap exit for every file that is not an object
	}
	// A first line that is a whole object, with more after it, is lines of
	// values (NDJSON), not one export.
	if i := bytes.IndexByte(head, '\n'); i >= 0 && len(bytes.TrimSpace(head[i+1:])) > 0 && json.Valid(head[:i]) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(head))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	version := false
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
			return version && perfEvents(dec)
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
}

// perfEvents reads the value of the events key, the decoder just past the
// key, and reports whether its first element is an event.
func perfEvents(dec *json.Decoder) bool {
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') || !dec.More() {
		return false
	}
	// Read the first element's keys as they come, so a large value in it
	// does not matter once its name and a distinctive key are seen.
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	named, distinct := false, false
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return false
		}
		named = named || key == perfEventNameKey
		distinct = distinct || slices.Contains(perfEventKeys, key)
		if named && distinct {
			return true
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
	}
	return false
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

// perfFile reports whether the absolute path a is a regular file inside the
// repository that is an export. A path outside the repository, a missing one
// and a file that is not regular are not.
//
// Each file opened is charged to the decision's budget. Past it the answer
// is true with s.perf.over set: the caller denies with scan-error, not as an
// export.
func (s *setup) perfFile(a string) bool {
	if _, ok := s.rel(a); !ok {
		return false
	}
	info, err := os.Stat(a)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	b := &s.perf
	if b.over {
		return true
	}
	b.files++
	b.bytes += int(min(info.Size(), perfHeadLimit))
	if b.files > perfMaxFiles || b.bytes > perfMaxBytes {
		b.over = true
		return true
	}
	return perfExport(a)
}

// perfDeny is the deny for the file perfFile answered true for.
func (s *setup) perfDeny(what, rel string) Decision {
	if s.perf.over {
		return deny(ReasonScanError, "%s more files than the gate checks for a Performance Analyzer export in one call "+
			"(%d files or %d MiB), so they were not vetted. Name fewer files.", what, perfMaxFiles, perfMaxBytes>>20)
	}
	return denyPerf(what, rel)
}

// denyPerf is the deny for the export at rel, reached by what (a command, or
// a search that read it). The message names the file and never its text.
func denyPerf(what, rel string) Decision {
	return deny(ReasonPerfExport, "%s %s, a Power BI Performance Analyzer export, which holds query text and visual titles that cannot be vetted. %s",
		what, displayPath(rel), hintPerf)
}

// perfExports decides the file operands of a reading or searching command
// in a shell line (the reading rule of walker.reaches, without the tree
// scan): one it can place, inside the repository, that is an export denies.
// A word it cannot place keeps today's rules. Each path is checked once.
func (w *walker) perfExports() *Decision {
	s := w.s
	for _, v := range w.views {
		if !v.cwd.known() {
			continue
		}
		_, search := searchers[v.name]
		reader := readers[v.name] || search
		operandReader := !reader && v.name != "" && v.name != "gh" && readsOperands(v.name)
		operands := operandWords(v)
		filter := jqFilter(v)
		for wi, xw := range v.words {
			readsInput := xw.redirect && xw.input
			if wi == filter || xw.here || xw.message && !w.redefines || xw.redirect && !readsInput ||
				(!reader && !readsInput && (!operandReader || !operands[wi])) {
				continue
			}
			abss, unknown := s.wordSpellings(v, xw)
			if unknown {
				continue
			}
			for _, a := range abss {
				if w.cache.memo("perf", a, s.perfFile) {
					rel, _ := s.rel(a)
					d := s.perfDeny("the command reads", rel)
					return &d
				}
			}
		}
	}
	return nil
}

// exportWatch is the reader of a tree scan: it passes each file through and
// notes the first that is an export, on the bytes the scanner already read,
// so the shape check is not another pass over the tree.
type exportWatch struct {
	r   scan.Reader
	hit string
}

func (e *exportWatch) Read(path string) ([]byte, error) {
	data, err := e.r.Read(path)
	if err == nil && e.hit == "" && perfExportShape(data) {
		e.hit = path
	}
	return data, err
}
