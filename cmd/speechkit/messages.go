package main

// User-facing UI messages. Centralized here for future i18n.
const (
	msgRecording         = "Speak now"
	msgProcessing        = "Recording stopped \u00b7 Transcribing"
	msgSaved             = "Saved"
	msgSaveFailed        = "Save failed: %v"
	msgFormParseError    = "Could not parse settings."
	msgUnsupportedModel  = "The selected model is not supported."
	msgUnsupportedStore  = "The selected store backend is not supported."
	msgUnsupportedVis    = "The selected visualizer is not supported."
	msgUnsupportedDesign = "The selected overlay design is not supported."
	msgPostgresDSNReq    = "A PostgreSQL connection string is required for the postgres backend."
	msgSummarizeInputMissing = "No text is available to summarize."
	msgHFTokenMissing    = "HF_TOKEN missing. Could not activate model."
	msgHFTokenRequired   = "Please enter a Hugging Face token."
	msgHFTokenSaved      = "Hugging Face token saved."
	msgHFTokenCleared    = "Hugging Face token cleared."
	msgModelUnreachable  = "Model is currently unreachable: %v"
)
