// Package edgesetup sets up an ESP32 voice satellite that is plugged in over
// USB: it finds the device, identifies its hardware, installs the matching
// firmware image from a release manifest, hands over Wi-Fi and links the
// device to an account with a single-use ticket.
//
// The package speaks the satellite's line protocol on the USB CDC console
// (`KBX <command>` in, ESP log lines out; see the firmware's
// docs/cdc-protocol.md) and the ESP32 ROM loader for identification when no
// application answers. It holds no service endpoints or account logic: the
// caller supplies the release manifest locations ([ReleaseSource]) and the
// ticket issuer ([Linker]), so a desktop app, a phone bridge or a browser
// gateway can reuse the same core.
//
// A device is identified by its hardware, never by its port name, its USB
// serial string or the board its running image was built for: the `KBX hw?`
// report probes the board, and the factory MAC identifies the unit.
//
// Credentials (Wi-Fi password, link ticket) are written to the device and
// never logged; [Event] messages carry outcomes only.
package edgesetup
