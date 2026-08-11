package provideropts

const (
	SchemaProviderOptions = "speechkit.provider_options.v1"
	ManifestUpdated       = "2026-08-11"

	ModalitySTT        = "stt"
	ModalityTTS        = "tts"
	ModalityVoiceAgent = "voice_agent"
)

func DefaultManifests() []ProviderOptionManifest {
	return []ProviderOptionManifest{
		deepgramSTTManifest(),
		openAISTTManifest("openai", "OpenAI", []string{"stt.openai.gpt-4o-transcribe", "stt.openai.whisper-1"}, "https://platform.openai.com/docs/guides/speech-to-text"),
		openAISTTManifest("groq", "Groq", []string{"stt.groq.whisper-large-v3-turbo"}, "https://console.groq.com/docs/speech-to-text"),
		openAISTTManifest("ollama", "Ollama", []string{"stt.ollama.gemma4-e4b-transcribe"}, "https://platform.openai.com/docs/guides/speech-to-text"),
		// "vps" is the self-hosted whisper-server adapter's own identifier, which
		// is what the resolver looks up; it has no catalog profile of its own.
		openAISTTManifest("vps", "Self-hosted whisper-server", nil, "https://github.com/ggerganov/whisper.cpp"),
		googleSTTManifest(),
		assemblyAISTTManifest(),
		openRouterSTTManifest(),
		huggingFaceSTTManifest(),
		localSTTManifest(),
		deepgramTTSManifest(),
		openAITTSManifest(),
		googleTTSManifest(),
		deepgramVoiceAgentManifest(),
		assemblyAIVoiceAgentManifest(),
		geminiVoiceAgentManifest(),
		openAIVoiceAgentManifest(),
	}
}

func ManifestsByProvider() map[string][]ProviderOptionManifest {
	out := map[string][]ProviderOptionManifest{}
	for _, manifest := range DefaultManifests() {
		out[manifest.Provider] = append(out[manifest.Provider], manifest)
	}
	return out
}

func FindManifest(provider, modality string) (ProviderOptionManifest, bool) {
	for _, manifest := range DefaultManifests() {
		if manifest.Provider == provider && manifest.Modality == modality {
			return manifest, true
		}
	}
	return ProviderOptionManifest{}, false
}

func deepgramSTTManifest() ProviderOptionManifest {
	return manifest("deepgram", "Deepgram", ModalitySTT, []string{"stt.deepgram.nova-3", "stt.deepgram.nova-3-diarization"}, []OptionSupport{
		// Deepgram Listen takes a language, including the "multi" value that
		// selects multilingual code-switching. Leaving it out of the manifest
		// made the resolver drop every configured language as unsupported, so
		// the picker in Settings could not influence the request at all.
		languageOption("language", "https://developers.deepgram.com/docs/multilingual-code-switching",
			"Multilanguage: send the literal language=multi. Verified 2026-08-11: code-switching is available on Nova-2, Nova-3 and Flux Multilingual; Flux instead uses model=flux-general-multi with language_hint. Deepgram also recommends endpointing=100 for code-switching in streaming."),
		unsupported(OptionDetectLanguage, TypeBool, "Detect language", "SpeechKit sends an explicit language (or \"multi\" for code-switching) and never requests Deepgram's detection."),
		native(OptionPunctuation, TypeBool, "Punctuation", "punctuate", "https://developers.deepgram.com/docs/punctuation"),
		native(OptionSmartFormat, TypeBool, "Smart format", "smart_format", "https://developers.deepgram.com/docs/smart-format"),
		native(OptionDictation, TypeBool, "Dictation punctuation", "dictation", "https://developers.deepgram.com/docs/dictation"),
		native(OptionFillerWords, TypeBool, "Filler words", "filler_words", "https://developers.deepgram.com/docs/filler-words"),
		native(OptionNumerals, TypeBool, "Numerals", "numerals", "https://developers.deepgram.com/docs/numerals"),
		native(OptionVocabularyBias, TypeBool, "Use vocabulary bias", "keyterm", "https://developers.deepgram.com/docs/keyterm"),
		native(OptionKeyterms, TypeStringList, "Static keyterms", "keyterm", "https://developers.deepgram.com/docs/keyterm"),
		native(OptionSpeakerDiarization, TypeBool, "Speaker diarization", "diarize_model", "https://developers.deepgram.com/docs/diarization"),
		native(OptionEndpointingMs, TypeInt, "Endpointing", "endpointing", "https://developers.deepgram.com/docs/endpointing"),
		unsupported(OptionPromptHint, TypeString, "Prompt hint", "Deepgram Listen uses keyterms/keywords for vocabulary bias."),
		unsupported(OptionTimestamps, TypeBool, "Word timestamps", "Deepgram returns word timing in verbose alternatives but SpeechKit does not expose it as a global toggle yet."),
	})
}

// openAISTTManifest describes the shared OpenAI-multipart transcription adapter.
// Every provider built on it — OpenAI, Groq, Ollama, the self-hosted
// whisper-server — posts the same four fields (file, language, model, prompt)
// and decodes only the text field back, so they share one capability statement.
func openAISTTManifest(provider, label string, profileIDs []string, evidence string) ProviderOptionManifest {
	return manifest(provider, label, ModalitySTT, profileIDs, []OptionSupport{
		languageOption("language", evidence,
			"Multilanguage: OMIT the field. Verified 2026-08-11 against openai/openai-openapi: language is optional and ISO-639-1, supplied only to \"improve accuracy and latency\". There is no auto or multi token -- sending one would be an invalid language code."),
		derived(OptionDetectLanguage, TypeBool, "Detect language", "Omit language so the provider auto-detects when supported.", evidence),
		native(OptionPromptHint, TypeString, "Prompt hint", "prompt", evidence),
		unsupported(OptionTimestamps, TypeBool, "Timestamps", "The adapter requests no timestamp granularities and keeps only the transcript text."),
		derived(OptionVocabularyBias, TypeBool, "Use vocabulary bias", "Dictionary terms are rendered into the prompt field.", evidence),
		unsupported(OptionKeyterms, TypeStringList, "Native keyterms", "OpenAI-compatible STT uses prompt hints, not native keyterms."),
		unsupported(OptionPunctuation, TypeBool, "Punctuation", "The transcription request carries no punctuation switch; the model decides."),
		unsupported(OptionSmartFormat, TypeBool, "Smart format", "Formatting is model/provider default for OpenAI-compatible transcription."),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Batch transcription has no server endpointing control."),
		unsupported(OptionSpeakerDiarization, TypeBool, "Speaker diarization", "OpenAI-compatible batch STT does not expose speaker labels in this adapter."),
	})
}

func googleSTTManifest() ProviderOptionManifest {
	return manifest("google", "Google Speech-to-Text", ModalitySTT, []string{"stt.google.latest-long", "stt.google.latest-long-diarization"}, []OptionSupport{
		languageOption("languageCode", "https://docs.cloud.google.com/speech-to-text/docs/multiple-languages",
			"Multilanguage: NOT open-ended. Verified 2026-08-11: v2 takes a language_codes LIST and \"You can list up to three languages for automatic language recognition\"; there is no auto value, and v1 requires a languageCode. Google therefore cannot express unconstrained multilanguage and needs candidate languages."),
		unsupported(OptionDetectLanguage, TypeBool, "Detect language", "SpeechKit's v1 REST adapter uses one languageCode; multi-language alternatives are not wired yet."),
		native(OptionPunctuation, TypeBool, "Automatic punctuation", "enableAutomaticPunctuation", "https://docs.cloud.google.com/speech-to-text/docs/reference/rest/v1/RecognitionConfig"),
		native(OptionVocabularyBias, TypeBool, "Use vocabulary bias", "speechContexts.phrases", "https://docs.cloud.google.com/speech-to-text/docs/reference/rest/v1/RecognitionConfig"),
		native(OptionKeyterms, TypeStringList, "Phrase hints", "speechContexts.phrases", "https://docs.cloud.google.com/speech-to-text/docs/reference/rest/v1/RecognitionConfig"),
		native(OptionSpeakerDiarization, TypeBool, "Speaker diarization", "diarizationConfig", "https://docs.cloud.google.com/speech-to-text/docs/reference/rest/v1/RecognitionConfig"),
		native(OptionTimestamps, TypeBool, "Word timestamps", "enableWordTimeOffsets", "https://docs.cloud.google.com/speech-to-text/docs/reference/rest/v1/RecognitionConfig"),
		unsupported(OptionSmartFormat, TypeBool, "Smart format", "Google STT v1 does not expose a Deepgram-style smart_format switch."),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Batch recognition has no server endpointing control."),
	})
}

func assemblyAISTTManifest() ProviderOptionManifest {
	return manifest("assemblyai", "AssemblyAI", ModalitySTT, []string{"stt.assemblyai.universal", "stt.assemblyai.universal-diarization"}, []OptionSupport{
		languageOption("language_code", "https://www.assemblyai.com/docs/api-reference/transcripts/submit",
			"Multilanguage: OMIT language_code. Verified 2026-08-11: \"If you dont specify a language, its detected automatically\", and language_detection \"is applied automatically when you dont specify a language_code\" (default true). True in-file code-switching is separate: language_codes plus language_detection_options.code_switching, and one of the values must be en."),
		native(OptionDetectLanguage, TypeBool, "Detect language", "language_detection", "https://www.assemblyai.com/docs/api-reference/transcripts/submit"),
		native(OptionPunctuation, TypeBool, "Punctuation", "punctuate", "https://www.assemblyai.com/docs/api-reference/transcripts/submit"),
		native(OptionSmartFormat, TypeBool, "Format text", "format_text", "https://www.assemblyai.com/docs/api-reference/transcripts/submit"),
		native(OptionContextPrompt, TypeString, "Context prompt", "prompt", "https://www.assemblyai.com/docs/streaming/prompting-and-keyterms"),
		native(OptionPrivacyRedaction, TypeBool, "PII redaction", "redact_pii", "https://www.assemblyai.com/docs/pre-recorded-audio/pii-redaction"),
		native(OptionVoiceFocus, TypeBool, "Voice focus", "voice_focus", "https://www.assemblyai.com/docs/speech-to-text/voice-focus"),
		native(OptionMedicalDomain, TypeBool, "Medical domain", "domain=medical-v1", "https://www.assemblyai.com/blog/introducing-medical-mode"),
		native(OptionSpeakerDiarization, TypeBool, "Speaker labels", "speaker_labels", "https://www.assemblyai.com/docs/pre-recorded-audio/label-speakers"),
		native(OptionVocabularyBias, TypeBool, "Use keyterms prompting", "keyterms_prompt", "https://www.assemblyai.com/docs/pre-recorded-audio/universal-3-pro/prompting"),
		native(OptionKeyterms, TypeStringList, "Native keyterms", "keyterms_prompt", "https://www.assemblyai.com/docs/pre-recorded-audio/universal-3-pro/prompting"),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Pre-recorded transcription has no endpointing control."),
	})
}

func openRouterSTTManifest() ProviderOptionManifest {
	return manifest("openrouter", "OpenRouter", ModalitySTT, []string{"stt.openrouter.whisper-1"}, []OptionSupport{
		languageOption("language", "https://openrouter.ai/docs/api-reference/overview",
			"Multilanguage: OMIT the field, inferred from OpenAI schema compatibility -- \"OpenRouter normalizes the schema across models and providers\". DERIVED, NOT VENDOR-VERIFIED: as of 2026-08-11 OpenRouter publishes no dedicated audio-transcription reference, so this entry needs a live roundtrip before it counts as verified."),
		derived(OptionDetectLanguage, TypeBool, "Detect language", "Omit language to let the routed model detect when supported.", "https://openrouter.ai/docs/api/api-reference/transcriptions/create-audio-transcriptions"),
		unsupported(OptionPromptHint, TypeString, "Prompt hint", "The current OpenRouter adapter uses the JSON transcription endpoint without prompt."),
		unsupported(OptionKeyterms, TypeStringList, "Native keyterms", "No native keyterm mapping is exposed by the current adapter."),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Batch transcription has no server endpointing control."),
	})
}

func huggingFaceSTTManifest() ProviderOptionManifest {
	return manifest("huggingface", "Hugging Face", ModalitySTT, []string{"stt.routed.whisper-large-v3"}, []OptionSupport{
		derived(OptionLanguage, TypeString, "Language", "Language is recorded in SpeechKit metadata; routed HF ASR payload is raw audio.", "https://huggingface.co/docs/inference-providers/tasks/automatic-speech-recognition"),
		unsupported(OptionDetectLanguage, TypeBool, "Detect language", "Model/provider default."),
		unsupported(OptionPromptHint, TypeString, "Prompt hint", "The current HF routed ASR adapter posts raw audio only."),
		unsupported(OptionKeyterms, TypeStringList, "Native keyterms", "No native keyterm mapping is exposed by the current adapter."),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Batch transcription has no server endpointing control."),
	})
}

func localSTTManifest() ProviderOptionManifest {
	return manifest("local", "Local STT", ModalitySTT, []string{"stt.local.whispercpp"}, []OptionSupport{
		// The bundled whisper-server speaks the OpenAI multipart shape, so both
		// values travel as their own request fields rather than being folded
		// into some other parameter.
		languageOption("language", "https://github.com/ggerganov/whisper.cpp/blob/master/examples/server/README.md",
			"Multilanguage: send the literal auto. Verified 2026-08-11: the server help reads \"-l LANG, --language LANG [en] spoken language (auto for auto-detect)\" -- the DEFAULT IS ENGLISH, so omitting the field pins English instead of detecting. This is why the sentinel is translated per provider rather than normalized away."),
		native(OptionPromptHint, TypeString, "Prompt hint", "prompt", "https://github.com/ggerganov/whisper.cpp"),
		derived(OptionVocabularyBias, TypeBool, "Use vocabulary bias", "Dictionary terms are rendered into the prompt field.", "https://github.com/ggerganov/whisper.cpp"),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "Local batch transcription uses SpeechKit VAD/recording controls."),
	})
}

func deepgramTTSManifest() ProviderOptionManifest {
	return manifest("deepgram", "Deepgram", ModalityTTS, []string{"tts.deepgram.aura-2"}, []OptionSupport{
		native(OptionVoice, TypeString, "Voice", "model", "https://developers.deepgram.com/docs/tts-models"),
		native(OptionAudioFormat, TypeString, "Audio format", "encoding/container", "https://developers.deepgram.com/docs/tts-encoding"),
		derived(OptionLanguage, TypeString, "Language", "Voice/model selection derives language.", "https://developers.deepgram.com/docs/tts-models"),
		unsupported(OptionSpeed, TypeFloat, "Speed", "Deepgram Aura-2 does not expose SpeechKit's generic speed control in this adapter."),
	})
}

func openAITTSManifest() ProviderOptionManifest {
	return manifest("openai", "OpenAI", ModalityTTS, []string{"tts.openai.tts-1-hd", "tts.openai.tts-1"}, []OptionSupport{
		native(OptionVoice, TypeString, "Voice", "voice", "https://platform.openai.com/docs/guides/text-to-speech"),
		native(OptionSpeed, TypeFloat, "Speed", "speed", "https://platform.openai.com/docs/guides/text-to-speech"),
		native(OptionAudioFormat, TypeString, "Audio format", "response_format", "https://platform.openai.com/docs/guides/text-to-speech"),
	})
}

func googleTTSManifest() ProviderOptionManifest {
	return manifest("google", "Google Text-to-Speech", ModalityTTS, []string{"tts.google.studio-o-de"}, []OptionSupport{
		native(OptionLanguage, TypeString, "Language", "voice.languageCode", "https://cloud.google.com/text-to-speech/docs/reference/rest/v1/text/synthesize"),
		native(OptionVoice, TypeString, "Voice", "voice.name", "https://cloud.google.com/text-to-speech/docs/reference/rest/v1/text/synthesize"),
		native(OptionSpeed, TypeFloat, "Speed", "audioConfig.speakingRate", "https://cloud.google.com/text-to-speech/docs/reference/rest/v1/text/synthesize"),
		native(OptionAudioFormat, TypeString, "Audio format", "audioConfig.audioEncoding", "https://cloud.google.com/text-to-speech/docs/reference/rest/v1/text/synthesize"),
	})
}

func deepgramVoiceAgentManifest() ProviderOptionManifest {
	return manifest("deepgram", "Deepgram Voice Agent", ModalityVoiceAgent, []string{"realtime.deepgram.voice-agent"}, []OptionSupport{
		native(OptionLanguage, TypeString, "Language", "agent.language", "https://developers.deepgram.com/docs/voice-agent-settings"),
		native(OptionLanguageHints, TypeStringList, "Language hints", "agent.listen.provider.language_hints", "https://developers.deepgram.com/docs/multilingual-voice-agent"),
		native(OptionKeyterms, TypeStringList, "Listen keyterms", "agent.listen.provider.keyterms", "https://developers.deepgram.com/docs/voice-agent-settings"),
		native(OptionVoice, TypeString, "Speak model", "agent.speak.provider.model", "https://developers.deepgram.com/docs/voice-agent-settings"),
		native(OptionContextPrompt, TypeString, "Context prompt", "agent.think.prompt", "https://developers.deepgram.com/docs/voice-agent-settings"),
		derived(OptionTurnDetection, TypeBool, "Turn detection", "Deepgram Voice Agent performs endpointing server-side.", "https://developers.deepgram.com/docs/voice-agent-settings"),
		unsupported(OptionResume, TypeBool, "Session resume", "Deepgram Voice Agent resume is not exposed through the current SpeechKit adapter."),
		unsupported(OptionPrivacyRedaction, TypeBool, "PII redaction", "Deepgram Voice Agent redaction is not exposed through the current SpeechKit adapter."),
		unsupported(OptionVoiceFocus, TypeBool, "Voice focus", "Deepgram Voice Agent has no AssemblyAI-style voice_focus switch in SpeechKit."),
		unsupported(OptionMedicalDomain, TypeBool, "Medical domain", "Deepgram medical-domain routing is not exposed through the current SpeechKit Voice Agent adapter."),
		unsupported(OptionReasoningEffort, TypeString, "Reasoning effort", "Deepgram Voice Agent reasoning is controlled by the configured think provider/model."),
		unsupported(OptionTranslation, TypeBool, "Translation", "Realtime translation is not exposed through the current SpeechKit Deepgram adapter."),
		unsupported(OptionTranscriptionOnly, TypeBool, "Transcription only", "Use Deepgram STT/Flux, not the full Voice Agent adapter, for transcription-only hosts."),
	})
}

func assemblyAIVoiceAgentManifest() ProviderOptionManifest {
	return manifest("assemblyai", "AssemblyAI Voice Agent", ModalityVoiceAgent, []string{"realtime.assemblyai.voice-agent"}, []OptionSupport{
		native(OptionKeyterms, TypeStringList, "Input keyterms", "session.input.keyterms", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api/session-configuration"),
		native(OptionVoice, TypeString, "Voice", "session.output.voice", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api/session-configuration"),
		native(OptionTurnDetection, TypeBool, "Turn detection", "session.input.turn_detection", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api/session-configuration"),
		native(OptionContextPrompt, TypeString, "Context prompt", "session.system_prompt", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api/session-configuration"),
		native(OptionResume, TypeBool, "Session resume", "session.resume", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api/events-reference"),
		derived(OptionLanguage, TypeString, "Language", "AssemblyAI Voice Agent infers language from audio and prompt context.", "https://www.assemblyai.com/docs/voice-agents/voice-agent-api"),
		unsupported(OptionLanguageHints, TypeStringList, "Language hints", "AssemblyAI Voice Agent uses audio and prompt context rather than explicit language_hints."),
		unsupported(OptionPrivacyRedaction, TypeBool, "PII redaction", "AssemblyAI Voice Agent PII redaction is not exposed through the current SpeechKit adapter."),
		unsupported(OptionVoiceFocus, TypeBool, "Voice focus", "AssemblyAI Voice Agent voice_focus is not exposed through the current SpeechKit adapter."),
		unsupported(OptionMedicalDomain, TypeBool, "Medical domain", "AssemblyAI Voice Agent medical-domain routing is not exposed through the current SpeechKit adapter."),
		unsupported(OptionReasoningEffort, TypeString, "Reasoning effort", "AssemblyAI Voice Agent reasoning is controlled by its session configuration and prompt."),
		unsupported(OptionTranslation, TypeBool, "Translation", "Realtime translation is not exposed through the current SpeechKit AssemblyAI adapter."),
		unsupported(OptionTranscriptionOnly, TypeBool, "Transcription only", "Use AssemblyAI streaming STT, not the full Voice Agent adapter, for transcription-only hosts."),
	})
}

func geminiVoiceAgentManifest() ProviderOptionManifest {
	return manifest("google", "Gemini Live", ModalityVoiceAgent, []string{"realtime.google.gemini-native-audio", "realtime.google.gemini-live-translate"}, []OptionSupport{
		derived(OptionLanguage, TypeString, "Language", "SpeechKit injects locale guidance into the system instruction.", "https://ai.google.dev/gemini-api/docs/live-guide"),
		native(OptionVoice, TypeString, "Voice", "speechConfig.voiceConfig", "https://ai.google.dev/gemini-api/docs/live-guide"),
		native(OptionTurnDetection, TypeBool, "Turn detection", "realtimeInputConfig.automaticActivityDetection", "https://ai.google.dev/gemini-api/docs/live-guide"),
		native(OptionContextPrompt, TypeString, "Context prompt", "systemInstruction", "https://ai.google.dev/gemini-api/docs/live-guide"),
		native(OptionReasoningEffort, TypeString, "Reasoning effort", "thinkingConfig.thinkingLevel", "https://ai.google.dev/gemini-api/docs/live-api/capabilities"),
		native(OptionResume, TypeBool, "Session resume", "sessionResumption", "https://ai.google.dev/gemini-api/docs/live-api/capabilities"),
		native(OptionTranslation, TypeBool, "Translation", "model", "https://ai.google.dev/gemini-api/docs/live-api/translation"),
		unsupported(OptionLanguageHints, TypeStringList, "Language hints", "Gemini Live uses prompt/locale guidance rather than native language_hints."),
		unsupported(OptionPrivacyRedaction, TypeBool, "PII redaction", "Gemini Live PII redaction is not exposed through the current SpeechKit adapter."),
		unsupported(OptionVoiceFocus, TypeBool, "Voice focus", "Gemini Live has no AssemblyAI-style voice_focus switch."),
		unsupported(OptionMedicalDomain, TypeBool, "Medical domain", "Gemini Live medical-domain routing is not exposed through the current SpeechKit adapter."),
		unsupported(OptionTranscriptionOnly, TypeBool, "Transcription only", "Gemini Live transcription-only sessions are not exposed through the current SpeechKit adapter."),
	})
}

func openAIVoiceAgentManifest() ProviderOptionManifest {
	return manifest("openai", "OpenAI Realtime", ModalityVoiceAgent, []string{"realtime.openai.gpt-realtime-2"}, []OptionSupport{
		native(OptionVoice, TypeString, "Voice", "audio.output.voice", "https://platform.openai.com/docs/guides/realtime"),
		native(OptionTurnDetection, TypeBool, "Turn detection", "audio.input.turn_detection", "https://platform.openai.com/docs/guides/realtime"),
		native(OptionEndpointingMs, TypeInt, "Silence duration", "silence_duration_ms", "https://platform.openai.com/docs/guides/realtime"),
		native(OptionDetectLanguage, TypeBool, "Input transcription", "audio.input.transcription", "https://platform.openai.com/docs/guides/realtime-transcription"),
		native(OptionContextPrompt, TypeString, "Context prompt", "instructions", "https://platform.openai.com/docs/guides/realtime"),
		native(OptionReasoningEffort, TypeString, "Reasoning effort", "reasoning.effort", "https://platform.openai.com/docs/guides/realtime-models-prompting"),
		unsupported(OptionLanguageHints, TypeStringList, "Language hints", "OpenAI Realtime language hints are not exposed through the current SpeechKit adapter."),
		unsupported(OptionResume, TypeBool, "Session resume", "OpenAI Realtime resumable reconnect is not exposed through the current SpeechKit adapter."),
		unsupported(OptionPrivacyRedaction, TypeBool, "PII redaction", "OpenAI Realtime PII redaction is not exposed through the current SpeechKit adapter."),
		unsupported(OptionVoiceFocus, TypeBool, "Voice focus", "OpenAI Realtime has no AssemblyAI-style voice_focus switch."),
		unsupported(OptionMedicalDomain, TypeBool, "Medical domain", "OpenAI Realtime medical-domain routing is not exposed through the current SpeechKit adapter."),
		unsupported(OptionTranslation, TypeBool, "Translation", "Realtime translation is not exposed through the current SpeechKit OpenAI adapter."),
		unsupported(OptionTranscriptionOnly, TypeBool, "Transcription only", "OpenAI transcription-only Realtime sessions are not exposed through the current SpeechKit adapter."),
	})
}

func manifest(provider, label, modality string, profileIDs []string, options []OptionSupport) ProviderOptionManifest {
	return ProviderOptionManifest{
		Schema:     SchemaProviderOptions,
		Updated:    ManifestUpdated,
		Provider:   provider,
		Label:      label,
		Modality:   modality,
		ProfileIDs: profileIDs,
		Options:    options,
	}
}

func native(id OptionID, typ OptionType, label, nativeKey, evidence string) OptionSupport {
	return support(id, typ, label, SupportNative, nativeKey, evidence, "")
}

// languageOption declares a provider's natively supported language option.
//
// It exists because the native key alone does not say how the provider
// expresses MULTILANGUAGE, and that differs per vendor: Deepgram takes a
// literal "multi", whisper.cpp needs "auto" because its default is English,
// and the OpenAI-compatible adapters express it by omitting the field. The
// notes carry that, verified against vendor documentation 2026-08-11.
func languageOption(nativeKey, evidence, multilanguageNotes string) OptionSupport {
	return support(OptionLanguage, TypeString, "Language", SupportNative, nativeKey, evidence, multilanguageNotes)
}

func derived(id OptionID, typ OptionType, label, notes, evidence string) OptionSupport {
	return support(id, typ, label, SupportDerived, "", evidence, notes)
}

func unsupported(id OptionID, typ OptionType, label, notes string) OptionSupport {
	return support(id, typ, label, SupportUnsupported, "", "", notes)
}

func support(id OptionID, typ OptionType, label string, status SupportStatus, nativeKey, evidence, notes string) OptionSupport {
	return OptionSupport{
		ID:          id,
		Status:      status,
		NativeKey:   nativeKey,
		Implemented: true,
		EvidenceURL: evidence,
		Notes:       notes,
		Definition: OptionDefinition{
			ID:       id,
			Label:    label,
			Type:     typ,
			Advanced: id != OptionLanguage,
		},
	}
}
