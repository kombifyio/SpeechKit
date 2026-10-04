package assemblyai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Agents is the documented stored-agent REST contract. It never retries a
// mutation. Hosts journal a unique Name before Create, then reconcile uncertain
// outcomes with List and delete resources after their voice session ends.
type Agents struct {
	APIKey     string
	HTTPClient *http.Client
}

// StoredAgent is the non-secret identity returned by create/list.
type StoredAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CustomLLM is a streaming OpenAI-compatible callback. APIKey must be a scoped
// callback credential, never an owner login credential.
type CustomLLM struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

func (a Agents) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return nil, live.ErrMissingAPIKey
	}
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://agents.assemblyai.com/v1/agents"+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", a.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safeClient.Do(req)
	if err != nil {
		return nil, errors.New("assemblyai agent: REST transport failed")
	}
	return response, nil
}

// Create provisions one stored configuration without retrying an uncertain mutation.
func (a Agents) Create(ctx context.Context, name string, cfg live.LiveConfig, llm CustomLLM) (StoredAgent, error) {
	parsed, err := url.Parse(llm.BaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimSpace(llm.Model) == "" || strings.TrimSpace(llm.APIKey) == "" || strings.TrimSpace(name) == "" {
		return StoredAgent{}, errors.New("assemblyai agent: complete HTTPS custom LLM binding is required")
	}
	if cfg.StoredAgentID != "" {
		return StoredAgent{}, errors.New("assemblyai agent: cannot create from an existing stored agent")
	}
	payload, ok := assemblyAISessionUpdate(cfg)["session"].(map[string]any)
	if !ok {
		return StoredAgent{}, errors.New("assemblyai agent: invalid session configuration")
	}
	payload["name"] = name
	payload["voice"] = map[string]string{"voice_id": assemblyAIVoice(cfg.Voice)}
	payload["llm"] = []CustomLLM{llm}
	if payload["system_prompt"] == nil {
		payload["system_prompt"] = "Respond through the configured registered assistant."
	}
	response, err := a.request(ctx, http.MethodPost, "", payload)
	if err != nil {
		return StoredAgent{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return StoredAgent{}, fmt.Errorf("assemblyai agent: create status %d", response.StatusCode)
	}
	var agent StoredAgent
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&agent) != nil || agent.ID == "" {
		return StoredAgent{}, errors.New("assemblyai agent: uncertain create acknowledgement")
	}
	return agent, nil
}

// List returns stored identities for exact-name reconciliation of uncertain creates.
func (a Agents) List(ctx context.Context) ([]StoredAgent, error) {
	response, err := a.request(ctx, http.MethodGet, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("assemblyai agent: list status %d", response.StatusCode)
	}
	var agents []StoredAgent
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&agents) != nil {
		return nil, errors.New("assemblyai agent: invalid list response")
	}
	return agents, nil
}

// Delete removes a validated stored configuration identity; an absent resource succeeds.
func (a Agents) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return errors.New("assemblyai agent: deletion identity is required")
	}
	response, err := a.request(ctx, http.MethodDelete, "/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("assemblyai agent: delete status %d", response.StatusCode)
	}
	return nil
}
