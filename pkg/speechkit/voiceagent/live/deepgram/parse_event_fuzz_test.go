package deepgram

import "testing"

func FuzzParseEvent(f *testing.F) {
	f.Add([]byte(`{"type":"ConversationText","role":"user","content":"hi"}`))
	f.Add([]byte(`{"type":"FunctionCallRequest","functions":[{"id":"1","name":"f","arguments":"{}"}]}`))
	f.Add([]byte(`{"type":"Error","description":"boom"}`))
	f.Add([]byte(``))
	f.Add([]byte(`{`))
	f.Add([]byte(`[]`))
	f.Add([]byte("\xff\xfe"))

	p := &Provider{}
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, swallow, err := p.parseEvent(data)
		if err == nil && !swallow && msg == nil {
			t.Fatal("parseEvent reported a deliverable event without a message")
		}
	})
}
