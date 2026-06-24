// Package main demonstrates provider/profile/model selection without live
// credentials. It uses a fake LiveProvider so the example is runnable in a
// public clone; production hosts can replace fakeProvider with Gemini,
// Deepgram, AssemblyAI, OpenAI, or a custom implementation.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

func main() {
	providerID := flag.String("provider", "assemblyai", "provider id: google, deepgram, assemblyai, openai, or auto")
	modelID := flag.String("model", "", "optional provider model override")
	preferred := flag.String("preferred", "", "comma-separated provider preference order used when -provider auto")
	requireNativeKeyterms := flag.Bool("require-native-keyterms", false, "require native vocabulary/keyterms support")
	flag.Parse()

	required := []live.LiveCapabilityFlag{
		live.LiveCapabilityRealtimeAudio,
		live.LiveCapabilityToolCalling,
	}
	requiredOptions := []provideropts.OptionID{}
	if *requireNativeKeyterms {
		required = append(required, live.LiveCapabilityNativeKeyterms)
		requiredOptions = append(requiredOptions, provideropts.OptionKeyterms)
	}
	provider := strings.TrimSpace(*providerID)
	if strings.EqualFold(provider, "auto") {
		provider = ""
	}
	plan, err := live.ResolveProviderIntent(live.ProviderIntent{
		Provider:             provider,
		Model:                strings.TrimSpace(*modelID),
		RequiredCapabilities: required,
		RequiredOptions:      requiredOptions,
		PreferredCapabilities: []live.LiveCapabilityFlag{
			live.LiveCapabilitySessionResume,
			live.LiveCapabilityNativeKeyterms,
		},
		SelectionPolicy: live.ProviderSelectionPolicy{
			PreferredProviders: providerPreference(provider, *preferred),
			AllowPreview:       true,
		},
	}, nil)
	if err != nil {
		printResolveError(err)
		printProviders()
		return
	}
	cfg := plan.LiveConfig()
	cfg.FrameworkPrompt = "You are a concise embedded voice assistant."
	printPlan(plan, cfg)

	session := &fakeProvider{name: cfg.Provider}
	if err := session.Connect(context.Background(), cfg); err != nil {
		panic(err)
	}
	defer session.Close()

	_ = session.SendText("Give me one sentence about provider switching.")
	msg, err := session.Receive(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s/%s model=%s -> %s\n", cfg.Provider, cfg.ProfileID, cfg.Model, msg.Text)
}

func printProviders() {
	for _, descriptor := range live.DefaultProviderDescriptors() {
		fmt.Printf("- %s (%s)\n", descriptor.Provider, descriptor.ProfileID)
	}
}

func providerPreference(provider, preferred string) []string {
	if strings.TrimSpace(preferred) != "" {
		return splitCSV(preferred)
	}
	if strings.TrimSpace(provider) != "" {
		return []string{provider}
	}
	return nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func printResolveError(err error) {
	var intentErr *live.ProviderIntentError
	if !errors.As(err, &intentErr) {
		fmt.Printf("provider intent failed: %v\n", err)
		return
	}
	fmt.Printf("provider intent failed: %v\n", intentErr)
	for _, rejection := range intentErr.RejectedProviders {
		fmt.Printf("rejected provider=%s profile=%s reason=%s missing_capabilities=%v missing_options=%v\n",
			rejection.Provider,
			rejection.ProfileID,
			rejection.Reason,
			rejection.MissingRequiredCapabilities,
			rejection.MissingRequiredOptions,
		)
	}
}

func printPlan(plan live.ResolvedProviderPlan, cfg live.LiveConfig) {
	fmt.Printf("selected provider=%s profile=%s model=%s reason=%q\n",
		plan.Provider,
		plan.ProfileID,
		plan.Model,
		plan.SelectionReason,
	)
	if plan.SelectedFallbackKind != "" {
		fmt.Printf("selected_fallback_kind=%s\n", plan.SelectedFallbackKind)
	}
	if cfg.FallbackModel != "" {
		fmt.Printf("same_provider_fallback_model=%s\n", cfg.FallbackModel)
	}
	for _, fallback := range plan.Fallbacks {
		fmt.Printf("fallback kind=%s provider=%s profile=%s model=%s reason=%q\n",
			fallback.Kind,
			fallback.Provider,
			fallback.ProfileID,
			fallback.Model,
			fallback.Reason,
		)
	}
	for _, rejection := range plan.RejectedProviders {
		fmt.Printf("rejected provider=%s profile=%s reason=%s missing_capabilities=%v missing_options=%v\n",
			rejection.Provider,
			rejection.ProfileID,
			rejection.Reason,
			rejection.MissingRequiredCapabilities,
			rejection.MissingRequiredOptions,
		)
	}
}

type fakeProvider struct {
	name string
	cfg  live.LiveConfig
}

func (p *fakeProvider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.cfg = cfg
	return nil
}

func (p *fakeProvider) SendAudio(chunk []byte) error { return nil }

func (p *fakeProvider) SendAudioStreamEnd() error { return nil }

func (p *fakeProvider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &live.LiveMessage{
		Text: fmt.Sprintf("fake session ready for %s using %s", p.cfg.Provider, p.cfg.Model),
		Done: true,
	}, nil
}

func (p *fakeProvider) SendText(text string) error { return nil }

func (p *fakeProvider) SendToolResponse(response live.ToolResponse) error { return nil }

func (p *fakeProvider) Close() error { return nil }

func (p *fakeProvider) Name() string { return p.name }
