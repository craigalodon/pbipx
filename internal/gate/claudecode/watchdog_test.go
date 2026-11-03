package claudecode

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestWatchdog: a decision that does not finish in time is a deny,
// whatever the decision code is doing.
func TestWatchdog(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	blocking := func(io.Reader, gate.Options) gate.Decision {
		<-block
		return gate.Decision{Allow: true}
	}
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := runWithin(strings.NewReader("{}"), &stdout, &stderr, gate.Options{}, 50*time.Millisecond, blocking)
	if code != ExitDeny {
		t.Fatalf("exit %d, want %d", code, ExitDeny)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("took %v", el)
	}
	if !strings.HasPrefix(stderr.String(), "pbipx: "+ReasonInternalError+":") {
		t.Errorf("stderr %q", stderr.String())
	}

	// A decision in time is rendered as made.
	stdout.Reset()
	stderr.Reset()
	quick := func(io.Reader, gate.Options) gate.Decision { return gate.Decision{Allow: true} }
	if code := runWithin(strings.NewReader("{}"), &stdout, &stderr, gate.Options{}, time.Minute, quick); code != ExitAllow {
		t.Errorf("exit %d, want %d", code, ExitAllow)
	}

	// A panic in the decision is a deny.
	panicking := func(io.Reader, gate.Options) gate.Decision { panic("x") }
	if code := runWithin(strings.NewReader("{}"), &stdout, &stderr, gate.Options{}, time.Minute, panicking); code != ExitDeny {
		t.Errorf("exit %d, want %d", code, ExitDeny)
	}

	// The watchdog fires well before the hook's own timeout, which lets
	// the call proceed.
	if Watchdog <= 0 || Watchdog > 150*time.Second {
		t.Errorf("Watchdog is %v", Watchdog)
	}
}
