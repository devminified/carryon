//go:build !race

package redact

// raceEnabled is false in a normal build. See race_enabled.go.
const raceEnabled = false
