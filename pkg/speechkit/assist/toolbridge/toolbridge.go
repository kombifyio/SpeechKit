// Package toolbridge adapts Assist-mode tools (assist.ToolMatcher /
// assist.ToolExecutor — the deterministic skill layer, e.g. the Home
// Assistant bridge) into agentkit.Tools, so realtime voice agents
// (Deepgram Voice Agent function calling, Gemini Live tools) can invoke
// the same skill implementations that one-shot Assist turns use.
//
// One tool vocabulary, two invocation styles: Assist matches on the raw
// transcript, the live agent calls a named function with a query
// argument. The bridge feeds the query through MatchTool so the skill's
// own gating (e.g. "did Home Assistant resolve an intent?") keeps
// working, and reports an unmatched query back to the agent instead of
// failing the call — the model then answers from its own knowledge.
package toolbridge

import (
	"context"
	"errors"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

var ErrMissingExecutor = errors.New("speechkit toolbridge: matcher and executor are required")

// Options describe one bridged tool.
type Options struct {
	// Name is the function name announced to the agent (e.g.
	// "home_assistant"). Required.
	Name string
	// Description tells the LLM when to call the tool. Required — a vague
	// description is the main cause of missed or spurious tool calls.
	Description string
	// Matcher gates the call: only a matched query is executed. Required.
	Matcher assist.ToolMatcher
	// Executor runs the matched call. Required.
	Executor assist.ToolExecutor
	// DefaultLocale is used when the agent omits the locale argument.
	DefaultLocale string
	// SessionKey threads the host's conversation identity (e.g.
	// "kbx:KBX-0001") into the AssistRequest.
	SessionKey string
}

// New builds an agentkit.Tool that forwards {"query", "locale"} function
// calls through the Assist matcher/executor pair.
func New(opts Options) (agentkit.Tool, error) {
	if strings.TrimSpace(opts.Name) == "" || strings.TrimSpace(opts.Description) == "" {
		return nil, errors.New("speechkit toolbridge: name and description are required")
	}
	if opts.Matcher == nil || opts.Executor == nil {
		return nil, ErrMissingExecutor
	}
	return &agentkit.FuncTool{
		ToolName:        opts.Name,
		ToolDescription: opts.Description,
		ToolSchema: agentkit.Schema{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The user's request, verbatim, in the user's language.",
				},
				"locale": map[string]any{
					"type":        "string",
					"description": "BCP-47 locale of the query (e.g. de-DE). Optional.",
				},
			},
			"required": []string{"query"},
		},
		Fn: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			query, _ := args["query"].(string)
			query = strings.TrimSpace(query)
			if query == "" {
				return map[string]any{"matched": false, "error": "empty query"}, nil
			}
			locale, _ := args["locale"].(string)
			if strings.TrimSpace(locale) == "" {
				locale = opts.DefaultLocale
			}

			req := speechkit.AssistRequest{Text: query, Locale: locale, SessionKey: opts.SessionKey}
			call, matched, err := opts.Matcher.MatchTool(ctx, req)
			if err != nil {
				return nil, err
			}
			if !matched {
				// Not an error: the skill declined (e.g. HA has no intent for
				// it). Tell the agent so it answers itself instead of retrying.
				return map[string]any{
					"matched": false,
					"note":    "the tool cannot handle this query; answer from your own knowledge",
				}, nil
			}
			result, err := opts.Executor.ExecuteTool(ctx, call)
			if err != nil {
				return nil, err
			}
			out := map[string]any{
				"matched": true,
				"text":    result.Text,
			}
			if strings.TrimSpace(result.SpeakText) != "" {
				out["speak_hint"] = result.SpeakText
			}
			if result.Action != "" {
				out["action"] = result.Action
			}
			if result.Kind != "" {
				out["kind"] = result.Kind
			}
			return out, nil
		},
	}, nil
}
