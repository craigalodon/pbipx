//go:build !race

package tmdl

// raceEnabled is set when the race detector is on, which slows the code
// under test several times over; timing bounds are not checked then.
const raceEnabled = false
