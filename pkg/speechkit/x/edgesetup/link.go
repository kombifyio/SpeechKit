package edgesetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// ErrTimeout reports that the device did not answer in time.
var ErrTimeout = errors.New("edgesetup: device did not answer in time")

// ErrClosed reports that the port went away (unplugged or rebooting).
var ErrClosed = errors.New("edgesetup: port closed")

// Link is the line protocol on an application console.
type Link struct {
	port   serial.Port
	lines  chan string
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex // serializes writes
	readMu sync.Mutex // one reader of lines at a time
}

// OpenLink opens the application console of a port.
//
// The TinyUSB console mirrors the log only while DTR is set. The built-in
// USB-Serial/JTAG resets the chip when DTR or RTS change, so both stay low.
func OpenLink(p Port) (*Link, error) {
	bits := &serial.ModemOutputBits{DTR: p.Mode == ModeApp, RTS: false}
	port, err := serial.Open(p.Name, &serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: bits})
	if err != nil {
		return nil, fmt.Errorf("edgesetup: open %s: %w", p.Name, err)
	}
	_ = port.SetReadTimeout(200 * time.Millisecond)
	l := &Link{port: port, lines: make(chan string, 4096), done: make(chan struct{})}
	go l.read()
	return l, nil
}

func (l *Link) read() {
	defer close(l.lines)
	buf := make([]byte, 4096)
	var pending []byte
	for {
		select {
		case <-l.done:
			return
		default:
		}
		n, err := l.port.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				i := bytes.IndexByte(pending, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimRight(stripANSI(string(pending[:i])), "\r ")
				pending = pending[i+1:]
				if line == "" {
					continue
				}
				select {
				case l.lines <- line:
				default:
					// A full buffer drops the oldest line: callers wait for fresh answers.
					select {
					case <-l.lines:
					default:
					}
					l.lines <- line
				}
			}
			if len(pending) > 8192 {
				pending = pending[:0]
			}
		}
		if err != nil {
			l.Close()
			return
		}
	}
}

// Close releases the port.
func (l *Link) Close() error {
	var err error
	l.once.Do(func() {
		close(l.done)
		err = l.port.Close()
	})
	return err
}

// Drain forgets lines that arrived before now.
func (l *Link) Drain() {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	for {
		select {
		case _, ok := <-l.lines:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// Send writes one command line. A secret argument must be passed as part of
// line only; Send never logs.
func (l *Link) Send(line string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.ContainsAny(line, "\r\n") {
		return errors.New("edgesetup: command contains a line break")
	}
	_, err := l.port.Write([]byte(line + "\n"))
	return err
}

// WriteRaw writes bytes without framing (the firmware image after `recv`).
func (l *Link) WriteRaw(data []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(data) > 0 {
		n, err := l.port.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

// Expect reads lines until match reports done, the context ends or timeout
// passes. match may return an error to stop with it.
func (l *Link) Expect(ctx context.Context, timeout time.Duration, match func(line string) (bool, error)) error {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ErrTimeout
		case line, ok := <-l.lines:
			if !ok {
				return ErrClosed
			}
			done, err := match(line)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// Ask sends a command and returns the first line that contains want.
func (l *Link) Ask(ctx context.Context, command, want string, timeout time.Duration) (string, error) {
	l.Drain()
	if err := l.Send(command); err != nil {
		return "", err
	}
	var found string
	err := l.Expect(ctx, timeout, func(line string) (bool, error) {
		if strings.Contains(line, want) {
			found = line
			return true, nil
		}
		return false, nil
	})
	return found, err
}

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
