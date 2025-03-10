//go:build !race
// +build !race

package router

// isRaceDetectorEnabled returns true if the race detector is enabled
func isRaceDetectorEnabled() bool {
	return false
}
