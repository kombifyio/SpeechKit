package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type readyTestProvider struct {
	name  string
	ready bool
}

func (p *readyTestProvider) Transcribe(context.Context, []byte, stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Provider: p.name}, nil
}

func (p *readyTestProvider) Name() string {
	return p.name
}

func (p *readyTestProvider) Health(context.Context) error {
	if p.ready {
		return nil
	}
	return fmt.Errorf("%s unavailable", p.name)
}

func (p *readyTestProvider) IsReady() bool {
	return p.ready
}

type healthOnlyTestProvider struct {
	name      string
	healthErr error
}

func (p *healthOnlyTestProvider) Transcribe(context.Context, []byte, stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Provider: p.name}, nil
}

func (p *healthOnlyTestProvider) Name() string {
	return p.name
}

func (p *healthOnlyTestProvider) Health(context.Context) error {
	return p.healthErr
}

func TestRuntimeAvailableProvidersFiltersUnreadyLocal(t *testing.T) {
	r := &router.Router{}
	r.SetLocal(&readyTestProvider{name: "local", ready: false})
	r.SetHuggingFace(&readyTestProvider{name: "huggingface", ready: true})

	got := runtimeAvailableProviders(t.Context(), r)
	want := []string{"huggingface"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime providers = %v, want %v", got, want)
	}
}

func TestRuntimeAvailableProvidersIncludesReadyLocal(t *testing.T) {
	r := &router.Router{}
	r.SetLocal(&readyTestProvider{name: "local", ready: true})
	r.SetHuggingFace(&readyTestProvider{name: "huggingface", ready: true})

	got := runtimeAvailableProviders(t.Context(), r)
	want := []string{"local", "huggingface"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime providers = %v, want %v", got, want)
	}
}

func TestProviderReadyFallsBackToHealth(t *testing.T) {
	if !providerReady(t.Context(), &healthOnlyTestProvider{name: "local"}) {
		t.Fatal("expected health-based provider readiness check to pass")
	}
	if providerReady(t.Context(), &healthOnlyTestProvider{name: "local", healthErr: fmt.Errorf("down")}) {
		t.Fatal("expected health-based provider readiness check to fail")
	}
}
