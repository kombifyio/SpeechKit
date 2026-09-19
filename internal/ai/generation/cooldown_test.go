package generation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedGenerator struct {
	provider string
	models   []Model
	errs     []error
	calls    int
	listings int
}

func (g *scriptedGenerator) Generate(context.Context, Request) (Result, error) {
	g.calls++
	if len(g.errs) == 0 {
		return Result{Text: "ok", Provider: g.provider}, nil
	}
	err := g.errs[0]
	g.errs = g.errs[1:]
	if err == nil {
		return Result{Text: "ok", Provider: g.provider}, nil
	}
	return Result{}, err
}

func (g *scriptedGenerator) Models(context.Context, ModelQuery) (Catalog, error) {
	g.listings++
	return Catalog{Models: g.models}, nil
}

func (g *scriptedGenerator) ProviderID() string { return g.provider }

// A Copilot account over its monthly quota was started and asked again for
// every meeting batch and every write-up pass, 13 s per failed attempt.
func TestCooldownSetsAProviderAsideAfterAQuotaFailure(t *testing.T) {
	quota := &Error{Kind: ErrorQuota, Provider: "github_copilot", Err: errors.New("You have exceeded your monthly quota")}
	inner := &scriptedGenerator{
		provider: "github_copilot",
		models:   []Model{{ID: "github_copilot/gpt-5.6-luna", Provider: "github_copilot"}},
		errs:     []error{quota},
	}
	now := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	cooldown := NewCooldown(inner)
	cooldown.now = func() time.Time { return now }

	if _, err := cooldown.Generate(context.Background(), Request{}); Kind(err) != ErrorQuota {
		t.Fatalf("first call kind = %q, want quota", Kind(err))
	}
	_, err := cooldown.Generate(context.Background(), Request{})
	if Kind(err) != ErrorQuota || !isResting(err) {
		t.Fatalf("resting call = %v (kind %q), want the quota failure again, marked as resting", err, Kind(err))
	}
	if inner.calls != 1 {
		t.Fatalf("provider asked %d times while resting, want 1", inner.calls)
	}
	catalog, err := cooldown.Models(context.Background(), ModelQuery{})
	if err != nil || len(catalog.Models) != 0 {
		t.Fatalf("resting catalog = %v, %v; want empty so callers size for the next model", catalog.Models, err)
	}
	if inner.listings != 0 {
		t.Fatalf("resting provider listed its models %d times", inner.listings)
	}

	now = now.Add(CooldownQuota)
	if _, err := cooldown.Generate(context.Background(), Request{}); err != nil {
		t.Fatalf("after the cooldown: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("provider asked %d times after the cooldown, want 2", inner.calls)
	}
}

func TestCooldownIgnoresFailuresTheNextRequestMayNotHit(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"transient", &Error{Kind: ErrorTransient, Err: errors.New("503")}},
		{"context limit", &Error{Kind: ErrorContextLimit, Err: errors.New("too long")}},
		{"invalid output", &Error{Kind: ErrorInvalidOutput, Err: errors.New("no json")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &scriptedGenerator{provider: "github_copilot", errs: []error{tc.err}}
			cooldown := NewCooldown(inner)
			_, _ = cooldown.Generate(context.Background(), Request{})
			if _, err := cooldown.Generate(context.Background(), Request{}); err != nil {
				t.Fatalf("second call: %v", err)
			}
			if inner.calls != 2 {
				t.Fatalf("provider asked %d times, want 2", inner.calls)
			}
		})
	}
}

func TestCooldownDoesNotRestAfterTheCallerCancelled(t *testing.T) {
	inner := &scriptedGenerator{provider: "github_copilot", errs: []error{&Error{Kind: ErrorAuthentication, Err: errors.New("sign in")}}}
	cooldown := NewCooldown(inner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = cooldown.Generate(ctx, Request{})
	if _, err := cooldown.Generate(context.Background(), Request{}); err != nil {
		t.Fatalf("a cancelled call must not set the provider aside: %v", err)
	}
}

// A request pinned to the local model must not start the Copilot CLI just to
// learn that Copilot does not serve it.
func TestChainRoutesAPinnedRequestWithoutListingOtherProviders(t *testing.T) {
	copilot := &scriptedGenerator{provider: "github_copilot"}
	local := &scriptedGenerator{models: []Model{{ID: "local/gemma", Provider: "local"}}}
	chain := NewChain(NewCooldown(copilot), &unnamedGenerator{local})

	result, err := chain.Generate(context.Background(), Request{ModelID: "local/gemma"})
	if err != nil {
		t.Fatalf("pinned request: %v", err)
	}
	if result.Text != "ok" || local.calls != 1 {
		t.Fatalf("local model calls = %d, result %+v", local.calls, result)
	}
	if copilot.calls != 0 || copilot.listings != 0 {
		t.Fatalf("copilot calls = %d, listings = %d; want neither", copilot.calls, copilot.listings)
	}
}

// unnamedGenerator hides ProviderID, like the bound generator that serves
// several providers.
type unnamedGenerator struct{ inner *scriptedGenerator }

func (g *unnamedGenerator) Generate(ctx context.Context, request Request) (Result, error) {
	return g.inner.Generate(ctx, request)
}

func (g *unnamedGenerator) Models(ctx context.Context, query ModelQuery) (Catalog, error) {
	return g.inner.Models(ctx, query)
}
