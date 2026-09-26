package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcputil "github.com/kombifyio/SpeechKit/cmd/speechkit-mcp/internal/util"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

func (a *speechkitMCP) status(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	status, err := a.client.Status(ctx)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(status), status, nil
}

func (a *speechkitMCP) configGet(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	cfg, err := a.client.Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(cfg), cfg, nil
}

func (a *speechkitMCP) providerList(ctx context.Context, req *mcp.CallToolRequest, in providerListInput) (*mcp.CallToolResult, any, error) {
	profiles, err := a.client.CatalogProfiles(ctx, in.Mode)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(profiles), profiles, nil
}

func (a *speechkitMCP) providerReadiness(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	ready, err := a.client.ProviderReadiness(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(ready), ready, nil
}

func (a *speechkitMCP) personasList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Personas(ctx)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Persona(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreatePersona(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdatePersona(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) personaDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeletePersona(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) rolesList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Roles(ctx)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Role(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreateRole(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdateRole(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) roleDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeleteRole(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) sequencesList(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Sequences(ctx)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceGet(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.Sequence(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceCreate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.CreateSequence(ctx, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceUpdate(ctx context.Context, req *mcp.CallToolRequest, in payloadInput) (*mcp.CallToolResult, any, error) {
	raw, err := a.client.UpdateSequence(ctx, in.ID, in.Payload)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(string(raw)), raw, nil
}

func (a *speechkitMCP) sequenceDelete(ctx context.Context, req *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
	if err := a.client.DeleteSequence(ctx, in.ID); err != nil {
		return nil, nil, err
	}
	return mcputil.TextResult(`{"deleted":true}`), map[string]any{"deleted": true}, nil
}

func (a *speechkitMCP) transcriptsList(ctx context.Context, req *mcp.CallToolRequest, in transcriptListInput) (*mcp.CallToolResult, any, error) {
	list, err := a.client.Transcripts(ctx, in.Limit)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(map[string]any{"transcripts": list}), map[string]any{"transcripts": list}, nil
}

func (a *speechkitMCP) transcriptGet(ctx context.Context, req *mcp.CallToolRequest, in numericIDInput) (*mcp.CallToolResult, any, error) {
	item, err := a.client.Transcript(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(item), item, nil
}

func (a *speechkitMCP) voiceAgentSessionSummary(ctx context.Context, req *mcp.CallToolRequest, in numericIDInput) (*mcp.CallToolResult, any, error) {
	summary, err := a.client.VoiceAgentSessionSummary(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(summary), summary, nil
}

func (a *speechkitMCP) vocabularyGet(ctx context.Context, req *mcp.CallToolRequest, in vocabularyInput) (*mcp.CallToolResult, any, error) {
	entries, err := a.client.VocabularyEntries(ctx, in.Language)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(map[string]any{"entries": entries}), map[string]any{"entries": entries}, nil
}

func (a *speechkitMCP) vocabularyReplace(ctx context.Context, req *mcp.CallToolRequest, in vocabularyInput) (*mcp.CallToolResult, any, error) {
	entries := make([]skclient.DictionaryEntry, 0, len(in.Entries))
	for _, raw := range in.Entries {
		entries = append(entries, skclient.DictionaryEntry{
			Spoken:    mcputil.StringMapValue(raw, "spoken"),
			Canonical: mcputil.StringMapValue(raw, "canonical"),
			Language:  firstNonEmpty(mcputil.StringMapValue(raw, "language"), in.Language),
			Source:    mcputil.StringMapValue(raw, "source"),
			Enabled:   true,
		})
	}
	out, err := a.client.ReplaceVocabularyEntries(ctx, in.Language, entries)
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(map[string]any{"entries": out}), map[string]any{"entries": out}, nil
}

func (a *speechkitMCP) transcribe(ctx context.Context, req *mcp.CallToolRequest, in transcribeInput) (*mcp.CallToolResult, any, error) {
	if a.opts.transport == "http" {
		return mcputil.TextResult("audio_path is disabled for HTTP MCP transport; use local stdio MCP for filesystem audio files."), nil, nil
	}
	result, err := a.client.TranscribeFile(ctx, in.AudioPath, skclient.TranscribeOptions{Language: in.Language, Model: in.Model})
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(result), result, nil
}

func (a *speechkitMCP) ttsSynthesize(ctx context.Context, req *mcp.CallToolRequest, in ttsInput) (*mcp.CallToolResult, any, error) {
	result, err := a.client.TTSSynthesize(ctx, skclient.TTSSynthesizeRequest{
		Text:   in.Text,
		Voice:  in.Voice,
		Locale: in.Locale,
		Format: in.Format,
		Speed:  in.Speed,
	})
	if err != nil {
		return nil, nil, err
	}
	return mcputil.JSONResult(result), result, nil
}
