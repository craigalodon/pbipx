package secret

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// plain is a synthetic value (CONTRIBUTING.md). No test output may contain it.
const plain = "Doe, Jane"

type exported struct {
	Name Value
}

type unexported struct {
	name Value
}

func TestFormattingNeverReveals(t *testing.T) {
	v := New(plain)
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%10s", "%-10v", "%T"}
	holders := map[string]any{
		"value":              v,
		"pointer":            &v,
		"slice":              []Value{v, v},
		"map":                map[string]Value{"k": v},
		"exported field":     exported{Name: v},
		"unexported field":   unexported{name: v},
		"pointer to struct":  &unexported{name: v},
		"nested in an error": fmt.Errorf("wrapped: %v", v),
	}
	for name, h := range holders {
		for _, verb := range verbs {
			got := fmt.Sprintf(verb, h)
			if strings.Contains(got, plain) || strings.Contains(got, fmt.Sprintf("%x", plain)) {
				t.Errorf("%s with %s revealed the value", name, verb)
			}
		}
	}
}

func TestStringAndGoString(t *testing.T) {
	v := New(plain)
	if v.String() != Redacted {
		t.Errorf("String() = %q", v.String())
	}
	if !strings.Contains(v.GoString(), Redacted) || strings.Contains(v.GoString(), plain) {
		t.Errorf("GoString() = %q", v.GoString())
	}
}

func TestMarshallingNeverReveals(t *testing.T) {
	v := New(plain)
	for name, h := range map[string]any{
		"value":          v,
		"exported field": exported{Name: v},
		"slice":          []Value{v},
	} {
		j, err := json.Marshal(h)
		if err != nil {
			t.Fatalf("json %s: %v", name, err)
		}
		if bytes.Contains(j, []byte(plain)) {
			t.Errorf("json %s revealed the value", name)
		}
	}
	x, err := xml.Marshal(exported{Name: v})
	if err != nil {
		t.Fatalf("xml: %v", err)
	}
	if bytes.Contains(x, []byte(plain)) {
		t.Errorf("xml revealed the value")
	}
}

func TestLoggingNeverReveals(t *testing.T) {
	var buf bytes.Buffer
	for _, h := range []slog.Handler{
		slog.NewTextHandler(&buf, nil),
		slog.NewJSONHandler(&buf, nil),
	} {
		slog.New(h).Info("finding", "value", New(plain), "err", errors.New("x"))
	}
	if strings.Contains(buf.String(), plain) {
		t.Errorf("log output revealed the value")
	}
}

func TestReveal(t *testing.T) {
	if got := Reveal(New(plain)); got != plain {
		t.Errorf("Reveal() did not return the plaintext")
	}
	if got := Reveal(Value{}); got != "" {
		t.Errorf("Reveal(zero) = %q, want empty", got)
	}
}
