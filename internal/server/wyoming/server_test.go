//go:build linux

package wyoming

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

type fakeTranscriber struct {
	gotBytes int
	gotLang  string
	result   *stt.Result
	err      error
}

func (f *fakeTranscriber) Route(_ context.Context, audio []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	f.gotBytes = len(audio)
	f.gotLang = opts.Language
	return f.result, f.err
}

type fakeSynthesizer struct {
	result *tts.Result
	err    error
}

func (f *fakeSynthesizer) Synthesize(_ context.Context, _ string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	return f.result, f.err
}

// dialServer starts a Wyoming server on a random loopback port and returns a
// connected client Reader/Writer plus a cleanup func.
func dialServer(t *testing.T, opts Options) (*Reader, *bufio.Writer, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(opts)
	go func() { _ = srv.Serve(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		cancel()
		_ = ln.Close()
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	cleanup := func() {
		_ = conn.Close()
		cancel()
		_ = ln.Close()
	}
	return NewReader(conn, DefaultMaxSegment), bufio.NewWriter(conn), cleanup
}

func writeClient(t *testing.T, bw *bufio.Writer, eventType string, data any) {
	t.Helper()
	ev, err := eventWithData(eventType, data)
	if err != nil {
		t.Fatalf("build %s: %v", eventType, err)
	}
	if err := WriteEvent(bw, ev); err != nil {
		t.Fatalf("write %s: %v", eventType, err)
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush %s: %v", eventType, err)
	}
}

func newSTTResult(text string) *stt.Result { return &stt.Result{Text: text} }

func newTTSResult(wav []byte) *tts.Result {
	return &tts.Result{Audio: wav, Format: "wav", SampleRate: 16000}
}

// writePCMChunks streams pcm as audio-chunk events of at most chunk bytes each.
func writePCMChunks(t *testing.T, bw *bufio.Writer, rate, width, channels int, pcm []byte, chunk int) {
	t.Helper()
	for off := 0; off < len(pcm); off += chunk {
		end := off + chunk
		if end > len(pcm) {
			end = len(pcm)
		}
		ev, err := audioChunkEvent(rate, width, channels, pcm[off:end])
		if err != nil {
			t.Fatalf("build audio chunk: %v", err)
		}
		if err := WriteEvent(bw, ev); err != nil {
			t.Fatalf("write audio chunk: %v", err)
		}
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush chunks: %v", err)
	}
}

// drainToAudioStop reads reply events until an audio-stop arrives (bounded).
func drainToAudioStop(t *testing.T, rd *Reader) bool {
	t.Helper()
	for i := 0; i < 64; i++ {
		ev, err := rd.ReadEvent()
		if err != nil {
			t.Fatalf("read during drain: %v", err)
		}
		if ev.Type == TypeAudioStop {
			return true
		}
	}
	return false
}

func TestServerDescribeReturnsInfo(t *testing.T) {
	info := Info{ASR: []AsrProgram{{Name: "speechkit"}}, TTS: []TtsProgram{{Name: "speechkit"}}}
	rd, bw, cleanup := dialServer(t, Options{Info: info})
	defer cleanup()

	writeClient(t, bw, TypeDescribe, struct{}{})
	ev, err := rd.ReadEvent()
	if err != nil {
		t.Fatalf("read info: %v", err)
	}
	if ev.Type != TypeInfo {
		t.Fatalf("type = %q, want %q", ev.Type, TypeInfo)
	}
	var got Info
	if err := decodeData(ev, &got); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if len(got.ASR) != 1 || len(got.TTS) != 1 {
		t.Errorf("info = %+v, want 1 asr + 1 tts", got)
	}
}

func TestServerSTTTurnBuffersToStop(t *testing.T) {
	fake := &fakeTranscriber{result: &stt.Result{Text: "hallo kombify"}}
	rd, bw, cleanup := dialServer(t, Options{STT: fake})
	defer cleanup()

	writeClient(t, bw, TypeTranscribe, Transcribe{Language: "de"})
	writeClient(t, bw, TypeAudioStart, AudioStart{Rate: 16000, Width: 2, Channels: 1})
	// Two canonical 16 kHz mono chunks.
	for i := 0; i < 2; i++ {
		chunk, err := audioChunkEvent(16000, 2, 1, make([]byte, 1600))
		if err != nil {
			t.Fatalf("chunk: %v", err)
		}
		if err := WriteEvent(bw, chunk); err != nil {
			t.Fatalf("write chunk: %v", err)
		}
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush chunks: %v", err)
	}
	writeClient(t, bw, TypeAudioStop, AudioStop{})

	ev, err := rd.ReadEvent()
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if ev.Type != TypeTranscript {
		t.Fatalf("type = %q, want %q", ev.Type, TypeTranscript)
	}
	var tr Transcript
	if err := decodeData(ev, &tr); err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	if tr.Text != "hallo kombify" {
		t.Errorf("text = %q, want %q", tr.Text, "hallo kombify")
	}
	if fake.gotBytes != 3200 {
		t.Errorf("router received %d bytes, want 3200 (whole utterance buffered to audio-stop)", fake.gotBytes)
	}
	if fake.gotLang != "de" {
		t.Errorf("router language = %q, want de", fake.gotLang)
	}
}

// TestServerSequentialTurnsOnOneConnection proves the per-connection state
// machine handles the real HA Assist reuse pattern: one describe handshake
// followed by an STT turn and then a TTS turn on the SAME connection, with the
// sttSession resetting cleanly between turns.
func TestServerSequentialTurnsOnOneConnection(t *testing.T) {
	stt := &fakeTranscriber{result: newSTTResult("wie spaet ist es")}
	pcm := make([]byte, 320)
	tts := &fakeSynthesizer{result: newTTSResult(pcmToWAV(pcm, 16000, 1, 2))}
	rd, bw, cleanup := dialServer(t, Options{
		Info: Info{ASR: []AsrProgram{{Name: "speechkit"}}, TTS: []TtsProgram{{Name: "speechkit"}}},
		STT:  stt,
		TTS:  tts,
	})
	defer cleanup()

	// 1. describe → info
	writeClient(t, bw, TypeDescribe, struct{}{})
	if ev, err := rd.ReadEvent(); err != nil || ev.Type != TypeInfo {
		t.Fatalf("describe reply = %v (%v), want info", ev, err)
	}

	// 2. STT turn → transcript
	writeClient(t, bw, TypeTranscribe, Transcribe{Language: "de"})
	writeClient(t, bw, TypeAudioStart, AudioStart{Rate: 16000, Width: 2, Channels: 1})
	writePCMChunks(t, bw, 16000, 2, 1, make([]byte, 3200), 1600)
	writeClient(t, bw, TypeAudioStop, AudioStop{})
	ev, err := rd.ReadEvent()
	if err != nil || ev.Type != TypeTranscript {
		t.Fatalf("stt reply = %v (%v), want transcript", ev, err)
	}
	var tr Transcript
	_ = decodeData(ev, &tr)
	if tr.Text != "wie spaet ist es" {
		t.Errorf("transcript = %q, want %q", tr.Text, "wie spaet ist es")
	}

	// 3. TTS turn on the same connection → audio-start/chunk(s)/stop
	writeClient(t, bw, TypeSynthesize, Synthesize{Text: "es ist drei uhr"})
	start, err := rd.ReadEvent()
	if err != nil || start.Type != TypeAudioStart {
		t.Fatalf("tts first reply = %v (%v), want audio-start", start, err)
	}
	if !drainToAudioStop(t, rd) {
		t.Fatal("never saw audio-stop on the reused connection")
	}
}

// TestServerSTTResamplesNonCanonicalInput exercises the real ESPHome satellite
// case: a 48 kHz stereo uplink must be downmixed + resampled to canonical
// 16 kHz mono S16LE before the batch router sees it (the slow path in
// canonicalPCM), not passed through raw.
func TestServerSTTResamplesNonCanonicalInput(t *testing.T) {
	fake := &fakeTranscriber{result: newSTTResult("ok")}
	rd, bw, cleanup := dialServer(t, Options{
		STT:          fake,
		DecodeLimits: audio.DecodeLimits{MaxDecodedAudioSeconds: 30},
	})
	defer cleanup()

	// 0.5 s of 48 kHz stereo S16LE silence = 48000*0.5 frames × 2ch × 2B.
	const inBytes = 48000 / 2 * 2 * 2 // 96000
	writeClient(t, bw, TypeTranscribe, Transcribe{Language: "en"})
	writeClient(t, bw, TypeAudioStart, AudioStart{Rate: 48000, Width: 2, Channels: 2})
	writePCMChunks(t, bw, 48000, 2, 2, make([]byte, inBytes), 8192)
	writeClient(t, bw, TypeAudioStop, AudioStop{})

	ev, err := rd.ReadEvent()
	if err != nil || ev.Type != TypeTranscript {
		t.Fatalf("reply = %v (%v), want transcript", ev, err)
	}
	// 0.5 s at 16 kHz mono S16LE = 8000 samples × 2B = 16000 bytes (±resampler
	// rounding). The key invariant: the router got the canonical stream, far
	// smaller than the 96000-byte 48 kHz stereo input, and S16LE-aligned.
	if fake.gotBytes >= inBytes {
		t.Errorf("router received %d bytes; expected downmixed+resampled (< %d)", fake.gotBytes, inBytes)
	}
	if fake.gotBytes < 15000 || fake.gotBytes > 17000 {
		t.Errorf("router received %d bytes, want ~16000 (0.5 s @16 kHz mono)", fake.gotBytes)
	}
	if fake.gotBytes%2 != 0 {
		t.Errorf("router received %d bytes; not S16LE-aligned", fake.gotBytes)
	}
}

// TestServerRejectsPeerOutsideAllowedCIDRs proves the only access control the
// adapter has (Wyoming has no in-protocol auth): a peer outside the allow-list
// is dropped before any Info is served.
func TestServerRejectsPeerOutsideAllowedCIDRs(t *testing.T) {
	_, cidr, err := net.ParseCIDR("10.0.0.0/8") // excludes 127.0.0.1
	if err != nil {
		t.Fatalf("parse cidr: %v", err)
	}
	rd, bw, cleanup := dialServer(t, Options{
		Info:         Info{ASR: []AsrProgram{{Name: "speechkit"}}},
		AllowedCIDRs: []*net.IPNet{cidr},
	})
	defer cleanup()

	// Best-effort describe; the server has already closed the connection.
	if ev, werr := eventWithData(TypeDescribe, struct{}{}); werr == nil {
		_ = WriteEvent(bw, ev)
		_ = bw.Flush()
	}
	if ev, rerr := rd.ReadEvent(); rerr == nil && ev.Type == TypeInfo {
		t.Fatal("disallowed peer received Info; allow-list not enforced")
	}
}

func TestServerTTSTurnStreamsAudio(t *testing.T) {
	// Provider returns a canonical 16 kHz mono WAV of 320 bytes of PCM.
	pcm := make([]byte, 320)
	wav := pcmToWAV(pcm, 16000, 1, 2)
	fake := &fakeSynthesizer{result: &tts.Result{Audio: wav, Format: "wav", SampleRate: 16000}}
	rd, bw, cleanup := dialServer(t, Options{TTS: fake})
	defer cleanup()

	writeClient(t, bw, TypeSynthesize, Synthesize{Text: "guten tag"})

	// Expect audio-start, >=1 audio-chunk, audio-stop.
	start, err := rd.ReadEvent()
	if err != nil || start.Type != TypeAudioStart {
		t.Fatalf("first reply = %v (%v), want audio-start", start, err)
	}
	var gotPCM int
	sawStop := false
	for i := 0; i < 10; i++ {
		ev, err := rd.ReadEvent()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		switch ev.Type {
		case TypeAudioChunk:
			gotPCM += len(ev.Payload)
		case TypeAudioStop:
			sawStop = true
		}
		if sawStop {
			break
		}
	}
	if !sawStop {
		t.Fatal("never saw audio-stop")
	}
	if gotPCM != len(pcm) {
		t.Errorf("streamed %d PCM bytes, want %d", gotPCM, len(pcm))
	}
}
