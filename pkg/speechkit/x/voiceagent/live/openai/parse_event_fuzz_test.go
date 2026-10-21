package openai

import "testing"

func FuzzParseEvent(f *testing.F) {
	f.Add([]byte(`{"type":"response.audio.delta","delta":"AQIDBA=="}`))
	f.Add([]byte(`{"type":"response.audio_transcript.delta","delta":"hello"}`))
	f.Add([]byte(`{"type":"session.created","event_id":"e1"}`))
	f.Add([]byte(`{"type":"error","error":{"code":"bad","message":"x"}}`))
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
