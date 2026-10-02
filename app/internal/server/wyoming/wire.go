// Package wyoming implements the Wyoming voice protocol so an ESPHome voice
// satellite (mediated by Home Assistant's Assist pipeline) can use
// speechkit-server as its STT and TTS backend.
//
// Wyoming is a peer-to-peer, event-driven protocol spoken over a raw TCP
// socket (also Unix socket / stdio upstream; this adapter uses TCP). Each event
// is a JSON header line terminated by '\n', optionally followed by a JSON
// "data" segment and a binary payload:
//
//	{"type":"audio-chunk","data_length":42,"payload_length":2048}\n
//	<42 bytes of JSON data><2048 bytes of PCM>
//
// wire.go is deliberately OS-agnostic and carries no build tag so the framing
// codec — the highest-risk part for interop — can be unit-tested on any
// platform. The TCP server, and the STT/TTS bridges that reach the kernel
// routers, are Linux-tagged (server target).
//
// Wire-format note: upstream producers vary on whether the "data" object is
// inline in the header line or a separate length-delimited segment. WriteEvent
// emits the separate-segment form used by the reference `wyoming` Python
// library (what Home Assistant speaks); ReadEvent accepts BOTH an inline "data"
// object and a "data_length" segment so this adapter interoperates with either.
// Validate against a captured wyoming-faster-whisper / wyoming-piper frame
// before relying on it in production (see wire_test.go).
package wyoming

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Event is one Wyoming message: a type, an optional structured data object
// (raw JSON), and an optional binary payload (PCM for audio events).
type Event struct {
	Type    string
	Data    json.RawMessage
	Payload []byte
}

// header is the JSON line that precedes the optional data + payload segments.
// "data" is accepted inline on read; on write the separate-segment form
// (data_length) is used, matching the reference wyoming library.
type header struct {
	Type          string          `json:"type"`
	Data          json.RawMessage `json:"data,omitempty"`
	DataLength    int             `json:"data_length,omitempty"`
	PayloadLength int             `json:"payload_length,omitempty"`
}

// Defaults for the read-side safety caps. A misbehaving or hostile peer must
// not be able to make the reader allocate unbounded memory from a single frame.
const (
	// maxHeaderLine caps the JSON header line length.
	maxHeaderLine = 128 << 10 // 128 KiB
	// DefaultMaxSegment caps the data + payload segment sizes when a Reader is
	// created without an explicit limit.
	DefaultMaxSegment = 1 << 20 // 1 MiB
)

// Reader reads framed Wyoming events from a buffered stream with bounded
// per-segment allocation.
type Reader struct {
	br         *bufio.Reader
	maxSegment int
}

// NewReader wraps r. maxSegment caps the data and payload segment sizes;
// values <= 0 use DefaultMaxSegment.
func NewReader(r io.Reader, maxSegment int) *Reader {
	if maxSegment <= 0 {
		maxSegment = DefaultMaxSegment
	}
	return &Reader{br: bufio.NewReaderSize(r, 16<<10), maxSegment: maxSegment}
}

// ReadEvent reads one framed event. It returns io.EOF at a clean end of stream
// (no partial frame buffered).
func (rd *Reader) ReadEvent() (*Event, error) {
	line, err := readLineLimited(rd.br, maxHeaderLine)
	if err != nil {
		return nil, err
	}
	var h header
	if err := json.Unmarshal(line, &h); err != nil {
		return nil, fmt.Errorf("wyoming: bad header line: %w", err)
	}
	if h.Type == "" {
		return nil, fmt.Errorf("wyoming: header missing type")
	}
	ev := &Event{Type: h.Type}

	switch {
	case len(h.Data) > 0:
		// Inline data object present in the header line.
		ev.Data = append(json.RawMessage(nil), h.Data...)
	case h.DataLength > 0:
		if h.DataLength > rd.maxSegment {
			return nil, fmt.Errorf("wyoming: data_length %d exceeds cap %d", h.DataLength, rd.maxSegment)
		}
		buf := make([]byte, h.DataLength)
		if _, err := io.ReadFull(rd.br, buf); err != nil {
			return nil, fmt.Errorf("wyoming: read data segment: %w", err)
		}
		ev.Data = buf
	}

	if h.PayloadLength > 0 {
		if h.PayloadLength > rd.maxSegment {
			return nil, fmt.Errorf("wyoming: payload_length %d exceeds cap %d", h.PayloadLength, rd.maxSegment)
		}
		buf := make([]byte, h.PayloadLength)
		if _, err := io.ReadFull(rd.br, buf); err != nil {
			return nil, fmt.Errorf("wyoming: read payload segment: %w", err)
		}
		ev.Payload = buf
	}
	return ev, nil
}

// WriteEvent writes one framed event to w and returns the write error, if any.
// It emits the separate-segment form: header line, then the data segment (when
// Data is non-empty), then the payload (when non-empty). The caller is
// responsible for flushing a buffered writer.
func WriteEvent(w io.Writer, ev *Event) error {
	h := header{Type: ev.Type}
	if len(ev.Data) > 0 {
		h.DataLength = len(ev.Data)
	}
	if len(ev.Payload) > 0 {
		h.PayloadLength = len(ev.Payload)
	}
	line, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("wyoming: marshal header: %w", err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return err
	}
	if len(ev.Data) > 0 {
		if _, err := w.Write(ev.Data); err != nil {
			return err
		}
	}
	if len(ev.Payload) > 0 {
		if _, err := w.Write(ev.Payload); err != nil {
			return err
		}
	}
	return nil
}

// readLineLimited reads up to and including the next '\n', erroring if the line
// exceeds max bytes before a newline is seen. The returned slice excludes the
// trailing '\n'.
func readLineLimited(br *bufio.Reader, maxBytes int) ([]byte, error) {
	var out []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(out)+len(chunk) > maxBytes {
			return nil, fmt.Errorf("wyoming: header line exceeds %d bytes", maxBytes)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			out = append(out, chunk...)
			continue
		}
		if err != nil {
			// io.EOF with no bytes buffered is a clean stream end.
			if errors.Is(err, io.EOF) && len(out) == 0 && len(chunk) == 0 {
				return nil, io.EOF
			}
			if errors.Is(err, io.EOF) && len(out)+len(chunk) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		out = append(out, chunk...)
		// Trim the trailing newline.
		return trimNewline(out), nil
	}
}

func trimNewline(b []byte) []byte {
	n := len(b)
	if n > 0 && b[n-1] == '\n' {
		n--
	}
	if n > 0 && b[n-1] == '\r' {
		n--
	}
	return b[:n]
}
