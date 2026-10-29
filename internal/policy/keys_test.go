package policy

import (
	"strings"
	"testing"
)

// shapedKeys are keys that hold a value: SSN-, phone-, digits- and
// name-shaped. None may appear in an error.
var shapedKeys = []string{"123-45-6789", "555-867-5309", "5558675309", "Doe, Jane"}

var shapedKeyParts = []string{"123", "45-6789", "555", "867", "5309", "Doe", "Jane"}

func TestShapedKeysAreNotPrinted(t *testing.T) {
	for _, key := range shapedKeys {
		for _, doc := range []string{
			header + "\n\"" + key + "\" = true\n",
			header + "\n[shapes]\n\"" + key + "\" = true\n",
			header + "\n[\"" + key + "\"]\n",
		} {
			_, err := parse([]byte(doc))
			if err == nil {
				t.Fatal("an unknown key loaded")
			}
			msg := err.Error()
			for _, part := range shapedKeyParts {
				if strings.Contains(msg, part) {
					t.Errorf("a shaped key reached the error")
				}
			}
			if !strings.Contains(msg, `"…"`) {
				t.Errorf("the key is not replaced by a placeholder")
			}
		}
	}
}
