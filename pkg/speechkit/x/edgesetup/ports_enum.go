//go:build !darwin || cgo

package edgesetup

import "go.bug.st/serial/enumerator"

func usbSerialPorts() ([]Port, error) {
	list, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	var out []Port
	for _, p := range list {
		if p != nil && p.IsUSB {
			out = append(out, Port{Name: p.Name, VID: p.VID, PID: p.PID, Serial: p.SerialNumber, Product: p.Product})
		}
	}
	return out, nil
}
