//go:build race

package capture

// raceEnabled reports whether the race detector is on. Under -race,
// sync.Pool deliberately drops about a quarter of Put calls, so reuse
// assertions must allow for those misses.
const raceEnabled = true
