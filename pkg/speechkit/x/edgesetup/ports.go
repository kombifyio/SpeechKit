package edgesetup

import (
	"fmt"
	"sort"
	"strings"
)

// Espressif USB identifiers.
const (
	VendorEspressif = "303A"
	// ProductTinyUSB is the application's own USB stack (CDC console plus audio).
	ProductTinyUSB = "8000"
	// ProductSerialJTAG is the chip's built-in USB-Serial/JTAG: the ROM loader of
	// every chip, and the application console of chips without a USB stack (C6).
	ProductSerialJTAG = "1001"
)

// Mode says which side of the device answers on a port.
type Mode string

const (
	// ModeApp is the application's CDC console (PID 8000).
	ModeApp Mode = "app"
	// ModeSerialJTAG is the built-in USB-Serial/JTAG (PID 1001): an application
	// console or the ROM loader; [Identify] tells them apart.
	ModeSerialJTAG Mode = "serial-jtag"
)

// Port is one attached Espressif USB device. Name changes on every replug and
// never identifies a device.
type Port struct {
	Name    string `json:"name"`
	VID     string `json:"vid"`
	PID     string `json:"pid"`
	Serial  string `json:"usb_serial,omitempty"`
	Product string `json:"product,omitempty"`
	Mode    Mode   `json:"mode"`
}

// ScanPorts lists the attached Espressif devices in a stable order.
func ScanPorts() ([]Port, error) {
	list, err := usbSerialPorts()
	if err != nil {
		return nil, fmt.Errorf("edgesetup: list serial ports: %w", err)
	}
	var ports []Port
	for _, port := range list {
		if !strings.EqualFold(port.VID, VendorEspressif) {
			continue
		}
		port.VID, port.PID = strings.ToUpper(port.VID), strings.ToUpper(port.PID)
		switch port.PID {
		case ProductTinyUSB:
			port.Mode = ModeApp
		case ProductSerialJTAG:
			port.Mode = ModeSerialJTAG
		default:
			continue
		}
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Name < ports[j].Name })
	return ports, nil
}
