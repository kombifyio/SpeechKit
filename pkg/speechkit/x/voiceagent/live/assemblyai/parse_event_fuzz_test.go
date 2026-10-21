package assemblyai

import "testing"

func FuzzParseEvent(f *testing.F) {
	f.Add([]byte(`{"type":"session.error","code":"bad_request","message":"invalid"}`))
	f.Add([]byte(`{"type":"transcript.user.delta","text":"hel"}`))
	f.Add([]byte(`{"type":"reply.audio","data":"AQID"}`))
	f.Add([]byte(``))
	f.Add([]byte(`{`))
	f.Add([]byte(`[]`))
	f.Add([]byte("\xff\xfe"))

	p := New()
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, swallow, err := p.parseEvent(data)
		if err == nil && !swallow && msg == nil {
			t.Fatal("parseEvent reported a deliverable event without a message")
		}
	})
}
