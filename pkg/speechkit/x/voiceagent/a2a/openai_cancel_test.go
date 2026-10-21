package a2a

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Sensitive authority boundary: retries of an already consumed final turn
// still check current consent before replay/correlation can return a result.
func TestCurrentAuthorityRevalidatedBeforeConsumedTurn(t *testing.T) {
	var executions atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executions.Add(1)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":{"parts":[{"kind":"text","text":"Ready."}]}}`)
	}))
	defer upstream.Close()
	agent, err := New(Config{Endpoint: upstream.URL, TargetAgentID: "bound", SessionID: "durable"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("scoped-callback", 4)
	handler, err := NewOpenAIHandler(context.Background(), agent, token, time.Now().Add(time.Minute), "en")
	if err != nil {
		t.Fatal(err)
	}
	finals := NewFinalTurnBinding("voice")
	finals.Observe("Hello", "item-1")
	handler.BindTurn = finals.Resolve
	granted := true
	handler.Revalidate = func(context.Context) error {
		if !granted {
			return errors.New("authority revoked")
		}
		return nil
	}
	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"Hello"}]}`))
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code
	}
	if post() != http.StatusOK {
		t.Fatal("authorized canonical turn did not complete")
	}
	granted = false
	if post() != http.StatusForbidden || executions.Load() != 1 {
		t.Fatal("consumed-turn retry bypassed current authority or repeated effects")
	}
}

// Regression: native interruption must cancel canonical agent work even when
// the vendor leaves its HTTP callback open. The uncertain turn cannot replay.
func TestNativeTurnCancellationStopsBoundAgentWithoutReplay(t *testing.T) {
	cancelled := make(chan struct{})
	started := make(chan struct{})
	var executions atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executions.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	agent, err := New(Config{Endpoint: upstream.URL, TargetAgentID: "bound", SessionID: "durable"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	token := strings.Repeat("scoped-callback", 4)
	handler, err := NewOpenAIHandler(ctx, agent, token, time.Now().Add(time.Minute), "en")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	finals := NewFinalTurnBinding("voice")
	finals.Observe("Hello", "item-1")
	handler.BindTurn = finals.Resolve
	callback := httptest.NewServer(handler)
	defer func() { cancel(); callback.Close() }()
	post := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, callback.URL, strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"Hello"}]}`))
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	responseReady := make(chan *http.Response, 1)
	go func() { responseReady <- post() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("registered request did not start")
	}
	handler.CancelTurn()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("native interruption left registered work running")
	}
	response := <-responseReady
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || strings.Contains(string(body), "[DONE]") {
		t.Fatal("cancelled agent turn was reported as a completed answer")
	}
	retry := post()
	_ = retry.Body.Close()
	if retry.StatusCode != http.StatusConflict || executions.Load() != 1 {
		t.Fatal("interrupted uncertain turn repeated registered execution")
	}
	for _, phase := range []string{"body", "correlation"} {
		t.Run(phase, func(t *testing.T) {
			bound := make(chan struct{})
			waiting, err := NewOpenAIHandler(ctx, agent, token, time.Now().Add(time.Minute), "en")
			if err != nil {
				t.Fatal(err)
			}
			waiting.BindTurn = func(turnCtx context.Context, _ string) (string, error) {
				close(bound)
				<-turnCtx.Done()
				return "", turnCtx.Err()
			}
			body, writer := io.Pipe()
			req := httptest.NewRequest(http.MethodPost, "/", body)
			req.Header.Set("Authorization", "Bearer "+token)
			finished := make(chan struct{})
			go func() { waiting.ServeHTTP(httptest.NewRecorder(), req); close(finished) }()
			if phase == "body" {
				// The first read proves cancellation was registered before decoding.
				if _, err := writer.Write([]byte("{")); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := writer.Write([]byte(`{"stream":true,"messages":[{"role":"user","content":"Later"}]}`)); err != nil {
					t.Fatal(err)
				}
				_ = writer.Close()
				<-bound
			}
			waiting.CancelTurn()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("cancellation left callback admission waiting")
			}
			_ = writer.Close()
			if executions.Load() != 1 {
				t.Fatal("canceled admission dispatched privileged agent work")
			}
		})
	}
}
