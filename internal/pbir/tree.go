package pbir

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/judge"
)

// A decoded document is a tree of these:
//
//   - object: members in file order, each key once;
//   - []any: an array;
//   - string;
//   - json.Number, bool or nil: never judged.
//
// Members are kept as a list, in file order, so a walk can report where
// it is. Desktop never writes a key twice in one object, and a lookup would
// see only one of the two, so a duplicate key makes the file unparseable
// (decoder.object) rather than letting the other member hide a context.
type object []member

type member struct {
	key string
	val any
}

// get returns the member named key; decode guarantees there is at most one.
func (o object) get(key string) (any, bool) {
	for _, m := range o {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

// getObject returns the member named key when it is an object.
func (o object) getObject(key string) (object, bool) {
	v, ok := o.get(key)
	if !ok {
		return nil, false
	}
	obj, ok := v.(object)
	return obj, ok
}

// getString returns the member named key when it is a string.
func (o object) getString(key string) (string, bool) {
	v, ok := o.get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// sorted returns the members ordered by key, so a walk is deterministic
// whatever order the file wrote them in.
func (o object) sorted() []member {
	out := make([]member, len(o))
	copy(out, o)
	sortMembers(out)
	return out
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// decode parses a report JSON document. Its errors are *judge.ParseError
// values that carry a line and structure only: encoding/json's own messages
// can quote the input, so none is passed through.
func decode(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, bom)
	if !utf8.Valid(data) {
		return nil, &judge.ParseError{Reason: finding.NotUTF8, Msg: "not UTF-8"}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	d := &decoder{dec: dec, data: data}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, d.fail(err, "data after the document")
	}
	return v, nil
}

type decoder struct {
	dec  *json.Decoder
	data []byte
}

// fail builds the parse error for err, at the line of the decoder's offset
// or of the syntax error's.
func (d *decoder) fail(err error, msg string) error {
	offset := d.dec.InputOffset()
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		offset = syn.Offset
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		offset = int64(len(d.data))
		msg = "unexpected end of input"
	}
	return &judge.ParseError{Reason: finding.UnparseableJSON, Line: lineAt(d.data, offset), Msg: msg}
}

func (d *decoder) value() (any, error) {
	tok, err := d.dec.Token()
	if err != nil {
		return nil, d.fail(err, "invalid JSON")
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return d.object()
		case '[':
			return d.array()
		}
		return nil, d.fail(nil, "unexpected delimiter")
	default:
		return t, nil
	}
}

func (d *decoder) object() (any, error) {
	obj := object{}
	seen := map[string]bool{}
	for d.dec.More() {
		tok, err := d.dec.Token()
		if err != nil {
			return nil, d.fail(err, "invalid JSON")
		}
		key, ok := tok.(string)
		if !ok {
			return nil, d.fail(nil, "object key is not a string")
		}
		if seen[key] {
			// Fail closed: a lookup sees one member of the two, and the
			// other could hold a context.
			return nil, d.fail(nil, "duplicate key in an object")
		}
		seen[key] = true
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		obj = append(obj, member{key: key, val: v})
	}
	if _, err := d.dec.Token(); err != nil {
		return nil, d.fail(err, "invalid JSON")
	}
	return obj, nil
}

func (d *decoder) array() (any, error) {
	arr := []any{}
	for d.dec.More() {
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
	}
	if _, err := d.dec.Token(); err != nil {
		return nil, d.fail(err, "invalid JSON")
	}
	return arr, nil
}

// lineAt is the 1-based line of a byte offset.
func lineAt(data []byte, offset int64) int {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}
	return 1 + bytes.Count(data[:offset], []byte{'\n'})
}
