package audio

import (
	"io"
	"sync"
	"testing"
)

func TestStreamPipeWriteRead(t *testing.T) {
	p := newStreamPipe()

	data := []byte{0x01, 0x02, 0x03, 0x04}
	p.Write(data)

	buf := make([]byte, 10)
	n, err := p.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 bytes, got %d", n)
	}
	for i, b := range data {
		if buf[i] != b {
			t.Errorf("byte %d: expected 0x%02x, got 0x%02x", i, b, buf[i])
		}
	}
}

func TestStreamPipeMultipleWrites(t *testing.T) {
	p := newStreamPipe()

	p.Write([]byte{0x01, 0x02})
	p.Write([]byte{0x03, 0x04})

	buf := make([]byte, 10)
	n, err := p.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 bytes, got %d", n)
	}
}

func TestStreamPipeClosedReturnsEOF(t *testing.T) {
	p := newStreamPipe()
	p.Close()

	buf := make([]byte, 10)
	_, err := p.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestStreamPipeDrainsBeforeEOF(t *testing.T) {
	p := newStreamPipe()
	p.Write([]byte{0xAA, 0xBB})
	p.Close()

	buf := make([]byte, 10)
	n, err := p.Read(buf)
	if err != nil {
		t.Fatalf("expected nil error on first read, got %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 bytes, got %d", n)
	}

	// Next read should return EOF.
	_, err = p.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF on second read, got %v", err)
	}
}

func TestStreamPipeBlocksUntilData(t *testing.T) {
	p := newStreamPipe()

	var n int
	var readErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 10)
		n, readErr = p.Read(buf)
	}()

	// Write after a short delay — the reader should be blocked waiting.
	p.Write([]byte{0x42})
	wg.Wait()

	if readErr != nil {
		t.Fatalf("unexpected error: %v", readErr)
	}
	if n != 1 {
		t.Fatalf("expected 1 byte, got %d", n)
	}
}

func TestStreamPipeCloseIdempotent(t *testing.T) {
	p := newStreamPipe()
	p.Close()
	p.Close() // Should not panic.

	buf := make([]byte, 10)
	_, err := p.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestStreamPipeWriteAfterClose(t *testing.T) {
	p := newStreamPipe()
	p.Close()
	// Write after close is a no-op (should not panic).
	p.Write([]byte{0x01})
}
