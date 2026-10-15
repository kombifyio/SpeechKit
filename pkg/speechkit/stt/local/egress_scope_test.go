package local

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Under device_only a routed dictation runs on the local provider without the
// router's internet probe opening any connection: the probe target, a
// stand-in for a public host, never sees a socket.
func TestDeviceOnlyDictationOpensNoProbeConnection(t *testing.T) {
	var probed atomic.Int32
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close() //nolint:errcheck // test cleanup
	go func() {
		for {
			c, err := probe.Accept()
			if err != nil {
				return
			}
			probed.Add(1)
			_ = c.Close()
		}
	}()

	whisper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello"})
	}))
	defer whisper.Close()

	netsec.Configure(speechkit.NetworkScopeDeviceOnly.EgressPolicy(false))
	t.Cleanup(func() { netsec.Configure(netsec.Policy{}) })

	p := New(Options{})
	p.BaseURL = whisper.URL
	p.ready.Store(true)
	r := &stt.Router{Strategy: stt.StrategyDynamic, PreferLocalUnderSecs: 10, ConnectivityProbe: probe.Addr().String()}
	r.SetLocal(p)

	res, err := r.Route(context.Background(), make([]byte, 3200), 1, stt.TranscribeOpts{})
	if err != nil || res.Text != "hello" {
		t.Fatalf("Route = %v, %v", res, err)
	}
	if n := probed.Load(); n != 0 {
		t.Fatalf("connectivity probe opened %d connection(s) under device_only", n)
	}
}
