package cascaded

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cancellationAgent struct{ entered, canceled chan struct{} }

func (a *cancellationAgent) Run(ctx context.Context, _ AgentInput) (AgentOutput, error) {
	close(a.entered)
	<-ctx.Done()
	close(a.canceled)
	return AgentOutput{Text: "late answer"}, nil
}

func TestSessionCancellationAbortsTextAgentAndRejectsMoreMedia(t *testing.T) {
	for _, closeProvider := range []bool{false, true} {
		t.Run(map[bool]string{false: "parent canceled", true: "provider closed"}[closeProvider], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			agent := &cancellationAgent{entered: make(chan struct{}), canceled: make(chan struct{})}
			p := NewProvider(Deps{STT: &fakeSTT{}, Agent: agent})
			defer p.Close() //nolint:errcheck
			if err := p.Connect(ctx, SessionConfig{}); err != nil {
				t.Fatal(err)
			}
			if err := p.SendText("hello"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-agent.entered:
			case <-time.After(time.Second):
				t.Fatal("text agent did not start")
			}
			if closeProvider {
				_ = p.Close()
			} else {
				cancel()
			}
			select {
			case <-agent.canceled:
			case <-time.After(time.Second):
				t.Fatal("text agent outlived session")
			}
			if p.SendText("later") == nil || p.SendAudio([]byte{1, 2}) == nil || p.SendAudioStreamEnd() == nil {
				t.Fatal("ended session accepted more media")
			}
			readCtx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer stop()
			for {
				msg, err := p.Receive(readCtx)
				if errors.Is(err, ErrClosed) || errors.Is(err, context.DeadlineExceeded) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if msg.OutputTranscript != "" || len(msg.Audio) > 0 {
					t.Fatal("late reply escaped ended session")
				}
			}
		})
	}
}
