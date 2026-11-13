//go:build linux

package core

// releaseChannelStable is the one release path of the server; the health
// payload reports it. A standalone server has no other channel.
const releaseChannelStable = "stable"

// resolveReleaseChannel returns the server's release channel. The standalone
// server has exactly one, so there is nothing to configure and nothing to refuse.
func resolveReleaseChannel() (string, error) {
	return releaseChannelStable, nil
}
