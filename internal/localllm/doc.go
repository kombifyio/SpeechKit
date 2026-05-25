// Package localllm manages the lifecycle of the local llama.cpp
// HTTP server SpeechKit ships alongside the desktop bundle for
// offline LLM inference. Owns process spawn, health-probing,
// graceful shutdown, and the OpenAI-compatible client wrapper the
// Assist + Voice Agent paths route to when Local is the selected
// provider.
//
// Audit 2026-05-24 maintainability sweep.
package localllm
