// Package agentkit provides a small Go harness for building SpeechKit Voice
// Agent hosts.
//
// It wraps the realtime Voice Agent runtime with a session type, a thread-safe
// tool registry, lifecycle hooks, and a swappable session memory interface.
// Use it when embedding SpeechKit into an agent host that needs predictable
// tool registration and prompt/session lifecycle boundaries.
package agentkit
