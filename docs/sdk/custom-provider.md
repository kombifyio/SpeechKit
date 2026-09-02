# Adding a Custom Provider

SpeechKit ships providers for the common STT, TTS and realtime backends, but the
framework is designed so a host can add its own without forking. A custom
provider has three parts, each optional beyond the first:

1. **Implement the SPI** — `stt.STTProvider`, `tts.Provider`, or
   `live.LiveProvider`.
2. **Prove conformance** — run the shared contract suite
   (`sttcontract`, `ttscontract`, `livecontract`) in your own test package.
3. **Register a catalog profile** — describe the provider as a
   `speechkit.ProviderProfile` and extend the catalog with
   `catalog.DefaultCatalog().With(...)`, so setup UIs, defaults, readiness
   and policy filtering treat it like a shipped provider.

This guide walks through an STT provider end to end and notes where TTS and
realtime providers differ.

## 1. Implement `stt.STTProvider`

```go
package acme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const Name = "acme"

type Provider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (p *Provider) Name() string { return Name }

// Capabilities is optional (stt.CapabilityReporter). Reporting it lets the
// contract suite and routing layers reason about what the backend supports.
func (p *Provider) Capabilities() []speechkit.Capability {
	return []speechkit.Capability{speechkit.CapabilitySTT, speechkit.CapabilityTranscription}
}

func (p *Provider) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acme: health %s", resp.Status)
	}
	return nil
}

func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/v1/transcribe", bytes.NewReader(audio))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "audio/wav")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if opts.Language != "" {
		req.Header.Set("X-Language", opts.Language)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("acme: transcribe %s", resp.Status)
	}
	var body struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	// Providers must stamp their own identity; routers and audit rely on it.
	return &stt.Result{Text: body.Text, Language: body.Language, Provider: Name}, nil
}

func (p *Provider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}
```

Rules the contract suite enforces, so build them in from the start:

- `Name()` is pure metadata: stable across calls, never touches the network.
- A successful `Transcribe` returns a non-nil `Result` with `Provider` set to
  `Name()`.
- HTTP 5xx becomes an error; a canceled context becomes an error.
- Audio arrives as WAV bytes (`speechkit.PCMToWAV` produces the same shape the
  device and server pipelines use). Do not assume raw PCM.
- Do not log audio or transcripts. See
  [`docs/server/local-only-guarantee.md`](../server/local-only-guarantee.md).

Public URLs should go through `pkg/speechkit/netsec` validation like the
shipped providers do; accept loopback only when the host explicitly opts in
(the contract suite drives an `httptest` server, so your constructor needs a
way to relax that for tests).

## 2. Run the conformance suite

Put the test in an external package (`package acme_test`) — the contract
packages import `stt`/`tts`/`live`, so an internal test would form a cycle.

```go
package acme_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/sttcontract"

	"example.com/host/acme"
)

func TestAcmeConformance(t *testing.T) {
	sttcontract.RunContract(t, sttcontract.Case{
		Name:         "acme",
		ExpectedName: acme.Name,
		WantText:     "hello world",
		NewProvider: func(baseURL string) stt.STTProvider {
			return &acme.Provider{BaseURL: baseURL}
		},
		Success: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello world"})
		},
	})
}
```

`RunContract` adds subtests for identity, capabilities, successful
transcription, server error and context cancellation. Provider-specific
behaviour (retries, streaming, diarization) belongs in your own tests next to
it; the shared suite only asserts what must hold for every provider.

| SPI | Suite | Entry point |
| --- | --- | --- |
| `stt.STTProvider` | `pkg/speechkit/stt/sttcontract` | `RunContract(t, Case)` |
| `tts.Provider` | `pkg/speechkit/tts/ttscontract` | `RunContract(t, Case)` |
| `live.LiveProvider` | `pkg/speechkit/voiceagent/live/livecontract` | `Run(t, Case)`, `RunReceiveCancellation(t, Case)` |

The shipped providers run the same suites in
`pkg/speechkit/stt/allproviders`, `pkg/speechkit/tts` and
`pkg/speechkit/voiceagent/live/allproviders`; copy the closest `Case` when the
wire format is similar.

## 3. Register a catalog profile

The catalog is what hosts present in setup screens, what `hostconfig` and the
server validate selections against, and what feeds readiness. It lives in
`pkg/speechkit/catalog` (the root `speechkit` package only defines the
`ProviderProfile` contract). Extend it rather than concatenating slices by
hand:

```go
import (
	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

extended, err := catalog.DefaultCatalog().With(speechkit.ProviderProfile{
	ID:            "stt.acme.whisper-turbo",
	Name:          "Acme Whisper Turbo",
	Mode:          speechkit.ModeDictation,
	ProviderKind:  speechkit.ProviderKindCloudProvider,
	ExecutionMode: speechkit.ExecutionModeSelfHostedHTTP,
	Capabilities:  []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilitySTT},
	Description:   "Acme's hosted Whisper endpoint.",
})
if err != nil {
	// catalog.ErrInvalidProfile: capabilities outside the mode contract, empty id, ...
	// catalog.ErrDuplicateProfileID: id already in the catalog.
	return err
}
```

What the catalog does for you:

- **Derives the provider id** from the profile id. Ids follow
  `<mode>.<provider>.<model>` — `stt.`, `tts.`, `realtime.`, `assist.`,
  `utility.`, `speaker.` — so `stt.acme.whisper-turbo` yields provider `acme`
  without a hard-coded mapping. Set `Provider` explicitly if your id does not
  follow the shape.
- **Fills auth and transport** from `ExecutionMode` via
  `catalog.ProviderProfileWithDefaults`. Override `AuthRequirement` /
  `Transport` when the derived class is wrong for your backend.
- **Validates against the mode contract** (`speechkit.ValidateProfileForMode`):
  a dictation profile cannot advertise TTS capabilities, a voice-agent profile
  needs either realtime audio or a pipeline fallback, and so on. Invalid
  profiles never enter the catalog.
- **Groups into the provider matrix** (`extended.ProviderMatrix()`,
  `extended.ProviderDefaults()`) and honours `speechkit.RuntimePolicy`
  (`extended.Filter(policy)`) — the same surfaces the built-ins use.

`With` returns a new catalog and leaves `catalog.DefaultCatalog()` untouched.
The built-in profile slice (`catalog.DefaultProviderProfiles()`) stays available
for callers that only need the shipped set. A host that wants to *replace* a
built-in filters `catalog.DefaultCatalog().Profiles()` and builds a fresh
catalog with `catalog.NewCatalog`.

The provider matrix display name for an unknown provider is its id; hosts that
want a prettier label map it in their own UI layer.

## 4. Wire it into routing

```go
router := &stt.Router{Strategy: stt.StrategyDynamic}
router.SetLocal(localProvider)
router.AddCloud(&acme.Provider{BaseURL: "https://stt.acme.example", APIKey: key})

// Hand the router to the kernel as a Transcriber for dictation.Runtime, or
// call router.Route directly. Per-request preference works by profile or
// provider id:
res, err := router.Route(ctx, wav, seconds, stt.TranscribeOpts{
	Language:          "de",
	ProviderProfileID: "stt.acme.whisper-turbo", // or just "acme"
})
```

`Router.OnProviderSelected` gives you per-instance audit of which provider
answered; `stt.AsTranscriber(router)` bridges it to
`speechkit.Transcriber` for `dictation.NewRuntime`.

## TTS and realtime differences

- **TTS:** implement `tts.Provider` (`Synthesize`, `Name`, `Kind`, `Health`).
  `Kind()` must return a `tts.ProviderKind`; `Router` uses it for local-only /
  cloud-only strategies. Profile ids start with `tts.`; mode is
  `speechkit.ModeTTS` with `CapabilityTTS`.
- **Realtime (Voice Agent):** implement `live.LiveProvider`. Wrap connection
  and session errors in the shared sentinels (`live.ErrNotConnected`,
  `live.ErrSessionNotReady`, `live.ErrMissingAPIKey`, `live.ErrMissingEndpoint`)
  so hosts can `errors.Is` across providers. Profile ids start with
  `realtime.`; mode is `speechkit.ModeVoiceAgent` with
  `CapabilityRealtimeAudio` (native) or `CapabilityPipelineFallback` +
  `CapabilitySessionSummary` (cascaded).

## Checklist

- [ ] SPI implemented; `Result.Provider` / `Name()` stable.
- [ ] Contract suite passes in an external test package.
- [ ] No audio or transcript in logs; secrets come from the host, never from
      the provider package.
- [ ] Catalog profile registers without `ErrInvalidProfile`.
- [ ] Routing preference by profile id resolves to your provider.

Related: [SDK surface boundary](../architecture/sdk-surface-boundary.md) ·
[Framework API](../speechkit-framework-api.md) ·
[Naming and contract roles](../architecture/sdk-surface-boundary.md#naming-and-contract-roles)
