package local

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// errForeignListener reports a listener on the whisper-server port that is
// not the child this provider started.
var errForeignListener = errors.New("the process answering on the whisper-server port is not this provider's whisper-server")

// chooseServerPort returns the loopback port the next child listens on. Port 0
// asks the OS for a free ephemeral port, so no other local process can bind it
// ahead of time; a fixed port must be free right now, because whatever already
// listens there is not ours.
func chooseServerPort(ctx context.Context, configured int) (int, error) {
	var lc net.ListenConfig
	if configured == 0 {
		l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return 0, fmt.Errorf("choose whisper-server port: %w", err)
		}
		addr, ok := l.Addr().(*net.TCPAddr)
		if err := l.Close(); err != nil {
			return 0, fmt.Errorf("choose whisper-server port: %w", err)
		}
		if !ok {
			return 0, fmt.Errorf("choose whisper-server port: unexpected listener address %v", l.Addr())
		}
		return addr.Port, nil
	}
	l, err := lc.Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(configured)))
	if err != nil {
		return 0, fmt.Errorf("whisper-server port %d is already in use by another process: %w", configured, err)
	}
	if err := l.Close(); err != nil {
		return 0, fmt.Errorf("whisper-server port %d: %w", configured, err)
	}
	return configured, nil
}

// serverIdentity lets readiness tell this provider's whisper-server from any
// other process on its port. whisper.cpp has no API key, but it serves static
// files from its --public directory; each start points that at a fresh
// private directory holding one random file, and only a listener that serves
// that file's content is the child.
type serverIdentity struct {
	dir       string
	publicArg string // dir as it can travel through argv
	name      string // URL path of the nonce file
	nonce     string
}

func newServerIdentity() (*serverIdentity, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b[:])
	dir, err := os.MkdirTemp("", "speechkit-whisper-")
	if err != nil {
		return nil, err
	}
	id := &serverIdentity{dir: dir, publicArg: dir, name: "/" + token[:32] + ".txt", nonce: token[32:]}
	// argv reaches whisper-server in the ANSI code page on Windows (see
	// whisperModelArgument), so a temp dir under a non-ASCII profile name
	// needs its short name.
	if !isASCII(dir) {
		id.publicArg = asciiShortPath(dir)
		if id.publicArg == "" && runtime.GOOS == "windows" {
			id.remove()
			return nil, fmt.Errorf("no ASCII path for the identity directory %q", dir)
		}
		if id.publicArg == "" {
			id.publicArg = dir
		}
	}
	if err := os.WriteFile(filepath.Join(dir, token[:32]+".txt"), []byte(id.nonce), 0o600); err != nil {
		id.remove()
		return nil, err
	}
	return id, nil
}

// confirm checks that baseURL serves this start's nonce. A nil identity (the
// platform could not set one up) confirms nothing and accepts.
func (id *serverIdentity) confirm(ctx context.Context, client *http.Client, baseURL string) error {
	if id == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+id.name, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // close failure is not actionable for readiness.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || subtle.ConstantTimeCompare(body, []byte(id.nonce)) != 1 {
		return errForeignListener
	}
	return nil
}

func (id *serverIdentity) remove() {
	if id == nil {
		return
	}
	_ = os.RemoveAll(id.dir)
}
