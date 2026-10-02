package wyoming

import "encoding/json"

// Wyoming event types used by the STT (asr) and TTS backends.
const (
	TypeDescribe   = "describe"
	TypeInfo       = "info"
	TypeTranscribe = "transcribe"
	TypeTranscript = "transcript"
	TypeAudioStart = "audio-start"
	TypeAudioChunk = "audio-chunk"
	TypeAudioStop  = "audio-stop"
	TypeSynthesize = "synthesize"
)

// Attribution is the model/service credit block present throughout the Wyoming
// info schema.
type Attribution struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Info is the reply to a `describe` event: it advertises the asr and/or tts
// programs this server exposes so Home Assistant can register STT/TTS entities.
type Info struct {
	ASR []AsrProgram `json:"asr,omitempty"`
	TTS []TtsProgram `json:"tts,omitempty"`
}

type AsrProgram struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Models      []AsrModel  `json:"models"`
}

type AsrModel struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Languages   []string    `json:"languages"`
}

type TtsProgram struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Voices      []TtsVoice  `json:"voices"`
}

type TtsVoice struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Languages   []string    `json:"languages"`
}

// Transcribe starts an STT turn; both fields are optional hints.
type Transcribe struct {
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
}

// Transcript is the STT result sent back after audio-stop.
type Transcript struct {
	Text string `json:"text"`
}

// AudioStart / AudioChunk / AudioStop carry the PCM stream. Chunk audio bytes
// travel in the Event.Payload, not in Data.
type AudioStart struct {
	Rate      int  `json:"rate"`
	Width     int  `json:"width"`
	Channels  int  `json:"channels"`
	Timestamp *int `json:"timestamp,omitempty"`
}

type AudioChunk struct {
	Rate      int  `json:"rate"`
	Width     int  `json:"width"`
	Channels  int  `json:"channels"`
	Timestamp *int `json:"timestamp,omitempty"`
}

type AudioStop struct {
	Timestamp *int `json:"timestamp,omitempty"`
}

// Synthesize requests TTS for Text in the optional Voice.
type Synthesize struct {
	Text  string      `json:"text"`
	Voice *SynthVoice `json:"voice,omitempty"`
}

type SynthVoice struct {
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
}

// eventWithData builds an Event whose Data segment is the JSON encoding of data.
func eventWithData(eventType string, data any) (*Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Event{Type: eventType, Data: raw}, nil
}

// decodeData unmarshals ev.Data into dst. A nil/empty Data is a no-op so a bare
// event (e.g. audio-stop with no fields) decodes to the zero value.
func decodeData(ev *Event, dst any) error {
	if ev == nil || len(ev.Data) == 0 {
		return nil
	}
	return json.Unmarshal(ev.Data, dst)
}

// audioChunkEvent builds an audio-chunk event carrying pcm in the payload.
func audioChunkEvent(rate, width, channels int, pcm []byte) (*Event, error) {
	ev, err := eventWithData(TypeAudioChunk, AudioChunk{Rate: rate, Width: width, Channels: channels})
	if err != nil {
		return nil, err
	}
	ev.Payload = pcm
	return ev, nil
}

// transcriptEvent builds a transcript reply.
func transcriptEvent(text string) (*Event, error) {
	return eventWithData(TypeTranscript, Transcript{Text: text})
}
