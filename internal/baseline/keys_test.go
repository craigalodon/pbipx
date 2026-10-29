package baseline

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
			"version = 1\n\"" + key + "\" = true\n",
			"version = 1\n[entries.\"fp1:00\"]\nseverity = \"warn\"\n\"" + key + "\" = 1\n",
			"version = 1\n[entries.\"" + key + "\"]\nseverity = \"block\"\n",
		} {
			path := writeFile(t, doc)
			_, err := Load(path)
			if err == nil {
				t.Fatal("the baseline loaded")
			}
			// The message names the file, whose temporary directory can hold
			// any digits; only the rest of the message is checked.
			msg := strings.ReplaceAll(err.Error(), path, "")
			for _, part := range shapedKeyParts {
				if strings.Contains(msg, part) {
					t.Errorf("a shaped key reached the error")
				}
			}
		}
		if PrintableKey(key) == key {
			t.Error("a shaped entry key is printable")
		}
	}
}
