//go:build darwin && !cgo

package edgesetup

import "errors"

// macOS lists USB serial devices through IOKit, which needs cgo.
func usbSerialPorts() ([]Port, error) {
	return nil, errors.New("listing USB devices on macOS needs a cgo build")
}
