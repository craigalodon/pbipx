package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWorkingCopyFedOptionInjection checks that a name a confined producer
// prints that starts with - or @ is an option (or an @file) to the program the
// runner feeds, not a name.
func TestWorkingCopyFedOptionInjection(t *testing.T) {
	m := newWCMachine(t)
	wc := gate.ReasonWorkingCopy
	m.run(t, []wcCase{
		{"echo --files0-from", "", wcShell("echo --files0-from=LL0 | xargs sort"), wc},
		{"echo -T", "", wcShell("echo -T LL | xargs tar -cO"), wc},
		{"printf -T", "", wcShell("printf '%s\\n' -T LL | xargs tar -cO"), wc},
		{"echo @file", "", wcShell("echo @LL | xargs 7z a x"), wc},
		{"echo plain", "", wcShell("echo a b | xargs wc -c"), ""},
		{"printf plain", "", wcShell("printf 'a\\n' | xargs echo"), ""},
	})
}
