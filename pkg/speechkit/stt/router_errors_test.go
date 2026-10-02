package stt

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// funcProvider adapts a function to STTProvider for failure-path tests.
type funcProvider struct {
	name string
	fn   func(ctx context.Context) (*Result, error)
}

func (f funcProvider) Transcribe(ctx context.Context, _ []byte, _ TranscribeOpts) (*Result, error) {
	return f.fn(ctx)
}
func (f funcProvider) Name() string                 { return f.name }
func (f funcProvider) Health(context.Context) error { return nil }

func TestRouterJoinsEveryProviderFailure(t *testing.T) {
	r := &Router{Strategy: StrategyCloudOnly}
	r.AddCloud(funcProvider{name: "a", fn: func(context.Context) (*Result, error) {
		return nil, &ProviderError{Provider: "a", Kind: ErrorKindRateLimit}
	}})
	r.AddCloud(funcProvider{name: "b", fn: func(context.Context) (*Result, error) {
		return nil, &ProviderError{Provider: "b", Kind: ErrorKindAuth}
	}})

	_, err := r.Route(context.Background(), []byte("x"), 1, TranscribeOpts{})
	if !errors.Is(err, ErrRateLimited) || !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want both provider failures reachable", err)
	}
}

func TestParallelRouterCancelsLoserAfterWinner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck // test cleanup

	loserStarted := make(chan struct{})
	loserCancelled := make(chan struct{})
	r := &Router{
		Strategy:             StrategyDynamic,
		PreferLocalUnderSecs: 10,
		ParallelCloud:        true,
		ConnectivityProbe:    ln.Addr().String(),
	}
	r.SetLocal(funcProvider{name: "local", fn: func(context.Context) (*Result, error) {
		<-loserStarted // win only once the loser is in flight
		return &Result{Text: "won", Provider: "local"}, nil
	}})
	r.AddCloud(funcProvider{name: "cloud", fn: func(ctx context.Context) (*Result, error) {
		close(loserStarted)
		<-ctx.Done()
		close(loserCancelled)
		return nil, ctx.Err()
	}})

	res, err := r.Route(context.Background(), []byte("x"), 1, TranscribeOpts{})
	if err != nil || res.Text != "won" {
		t.Fatalf("Route = %v, %v", res, err)
	}
	select {
	case <-loserCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("losing provider was not cancelled")
	}
}
