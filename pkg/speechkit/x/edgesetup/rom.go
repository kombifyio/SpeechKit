package edgesetup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"go.bug.st/serial"
)

// The ESP32 ROM loader serial protocol (SLIP framed): enough to identify the
// chip and read its factory MAC when no application answers. Register
// addresses follow Espressif's esptool chip definitions.
const (
	romSync    = 0x08
	romReadReg = 0x0A
	slipEnd    = 0xC0
	slipEsc    = 0xDB

	chipMagicReg = 0x40001000
)

type romChip struct {
	name   string
	magic  uint32
	macReg uint32
}

var romChips = []romChip{
	{name: "esp32s3", magic: 0x00000009, macReg: 0x60007000 + 0x044},
	{name: "esp32c6", magic: 0x2CE0806F, macReg: 0x600B0800 + 0x044},
}

// IdentifyROM asks the ROM loader of a USB-Serial/JTAG port for the chip and
// factory MAC. The chip stays in the loader; DTR and RTS stay low so the
// chip is not reset.
func IdentifyROM(ctx context.Context, p Port) (Identity, error) {
	port, err := serial.Open(p.Name, &serial.Mode{BaudRate: 115200, InitialStatusBits: &serial.ModemOutputBits{}})
	if err != nil {
		return Identity{}, fmt.Errorf("edgesetup: open %s: %w", p.Name, err)
	}
	defer port.Close()
	_ = port.SetReadTimeout(100 * time.Millisecond)
	rom := &romLink{port: port}

	syncData := append([]byte{0x07, 0x07, 0x12, 0x20}, bytes.Repeat([]byte{0x55}, 32)...)
	synced := false
	for attempt := 0; attempt < 6 && !synced; attempt++ {
		if err := ctx.Err(); err != nil {
			return Identity{}, err
		}
		if _, err := rom.command(romSync, syncData, 300*time.Millisecond); err == nil {
			synced = true
		}
	}
	if !synced {
		return Identity{}, errors.New("edgesetup: the ROM loader did not answer")
	}
	rom.drain()
	magic, err := rom.readReg(chipMagicReg)
	if err != nil {
		return Identity{}, err
	}
	for _, chip := range romChips {
		if chip.magic != magic {
			continue
		}
		low, err := rom.readReg(chip.macReg)
		if err != nil {
			return Identity{}, err
		}
		high, err := rom.readReg(chip.macReg + 4)
		if err != nil {
			return Identity{}, err
		}
		mac := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", byte(high>>8), byte(high), byte(low>>24), byte(low>>16), byte(low>>8), byte(low))
		id := Identity{Chip: chip.name, MAC: mac, Match: MatchUnknown, Via: "rom", ROMLoader: true}
		if chip.name == "esp32c6" {
			// One supported C6 board: the chip names the board.
			id.Board = BoardC6AMOLED
		}
		return id, nil
	}
	return Identity{}, fmt.Errorf("edgesetup: unsupported chip (magic 0x%08x)", magic)
}

type romLink struct {
	port serial.Port
	buf  []byte
}

func (r *romLink) drain() {
	tmp := make([]byte, 256)
	for i := 0; i < 5; i++ {
		if n, _ := r.port.Read(tmp); n == 0 {
			return
		}
	}
	r.buf = r.buf[:0]
}

func (r *romLink) readReg(addr uint32) (uint32, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, addr)
	return r.command(romReadReg, data, time.Second)
}

// command sends one request and returns the value field of its response.
func (r *romLink) command(op byte, data []byte, timeout time.Duration) (uint32, error) {
	pkt := make([]byte, 8, 8+len(data))
	pkt[0], pkt[1] = 0x00, op
	binary.LittleEndian.PutUint16(pkt[2:], uint16(len(data)))
	pkt = append(pkt, data...) // checksum 0: only data-carrying flash writes use it
	if _, err := r.port.Write(slipEncode(pkt)); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, err := r.frame(deadline)
		if err != nil {
			return 0, err
		}
		if len(frame) < 10 || frame[0] != 0x01 || frame[1] != op {
			continue
		}
		size := int(binary.LittleEndian.Uint16(frame[2:4]))
		value := binary.LittleEndian.Uint32(frame[4:8])
		body := frame[8:]
		if size > len(body) || size < 2 {
			continue
		}
		status := body[size-4:]
		if size < 4 {
			status = body[size-2:]
		}
		if status[0] != 0 {
			return 0, fmt.Errorf("edgesetup: ROM loader error 0x%02x", status[1])
		}
		return value, nil
	}
	return 0, ErrTimeout
}

func (r *romLink) frame(deadline time.Time) ([]byte, error) {
	tmp := make([]byte, 512)
	for time.Now().Before(deadline) {
		if start := bytes.IndexByte(r.buf, slipEnd); start >= 0 {
			if end := bytes.IndexByte(r.buf[start+1:], slipEnd); end >= 0 {
				raw := r.buf[start+1 : start+1+end]
				r.buf = r.buf[start+1+end+1:]
				if len(raw) == 0 {
					continue
				}
				return slipDecode(raw), nil
			}
		}
		n, err := r.port.Read(tmp)
		if err != nil {
			return nil, err
		}
		r.buf = append(r.buf, tmp[:n]...)
	}
	return nil, ErrTimeout
}

func slipEncode(p []byte) []byte {
	out := []byte{slipEnd}
	for _, b := range p {
		switch b {
		case slipEnd:
			out = append(out, slipEsc, 0xDC)
		case slipEsc:
			out = append(out, slipEsc, 0xDD)
		default:
			out = append(out, b)
		}
	}
	return append(out, slipEnd)
}

func slipDecode(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == slipEsc && i+1 < len(p) {
			i++
			if p[i] == 0xDC {
				out = append(out, slipEnd)
			} else {
				out = append(out, slipEsc)
			}
			continue
		}
		out = append(out, p[i])
	}
	return out
}
