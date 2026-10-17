//go:build race

package ui

// raceOn reports a -race build, whose sync.Pool drops items at random.
const raceOn = true
