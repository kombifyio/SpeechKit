package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"
)

func testClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func TestCallOpenAICompatible_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var req oaiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "Hello" {
			t.Errorf("messages = %+v", req.Messages)
		}

		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message:      struct{ Content string `json:"content"` }{Content: "Hi there"},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 42},
		})
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hello")}},
		},
	}

	resp, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "test-key", "test-model", mr)
	if err != nil {
		t.Fatalf("callOpenAICompatible: %v", err)
	}
	if resp.Message == nil {
		t.Fatal("expected message in response")
	}
	if text := resp.Text(); text != "Hi there" {
		t.Errorf("text = %q, want 'Hi there'", text)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finishReason = %q", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 42 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestCallOpenAICompatible_ModelRoleMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req oaiRequest
		json.Unmarshal(body, &req)

		// "model" role should be mapped to "assistant"
		for _, msg := range req.Messages {
			if msg.Role == "model" {
				t.Error("model role should be mapped to assistant")
			}
		}
		if len(req.Messages) < 2 || req.Messages[1].Role != "assistant" {
			t.Errorf("expected assistant role, got messages: %+v", req.Messages)
		}

		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{Message: struct{ Content string `json:"content"` }{Content: "ok"}},
			},
		})
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("q")}},
			{Role: ai.RoleModel, Content: []*ai.Part{ai.NewTextPart("a")}},
		},
	}

	_, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "k", "m", mr)
	if err != nil {
		t.Fatalf("callOpenAICompatible: %v", err)
	}
}

func TestCallOpenAICompatible_ConfigApplied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req oaiRequest
		json.Unmarshal(body, &req)

		if req.MaxTokens != 256 {
			t.Errorf("max_tokens = %d, want 256", req.MaxTokens)
		}
		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Errorf("temperature = %v, want 0.7", req.Temperature)
		}

		json.NewEncoder(w).Encode(oaiResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{Message: struct{ Content string `json:"content"` }{Content: "ok"}},
			},
		})
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
		Config: &ai.GenerationCommonConfig{
			MaxOutputTokens: 256,
			Temperature:     0.7,
		},
	}

	_, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "k", "m", mr)
	if err != nil {
		t.Fatalf("callOpenAICompatible: %v", err)
	}
}

func TestCallOpenAICompatible_ErrorStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"error":"invalid api key"}`},
		{"rate limit", 429, `{"error":"rate limit"}`},
		{"server error", 500, `{"error":"internal"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			mr := &ai.ModelRequest{
				Messages: []*ai.Message{
					{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
				},
			}

			_, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "k", "m", mr)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCallOpenAICompatible_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(oaiResponse{})
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "k", "m", mr)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestCallOpenAICompatible_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := callOpenAICompatible(context.Background(), testClient(), server.URL, "k", "m", mr)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCallOpenAICompatible_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{
			{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("test")}},
		},
	}

	_, err := callOpenAICompatible(ctx, testClient(), server.URL, "k", "m", mr)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
