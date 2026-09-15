# Meeting Notes Go Prompt

Stable prompt URL: `https://speechkit.cc/getting-started/agents/meeting-notes-go.md`

This starter demonstrates embedded Go composition with synthetic input. It does
not require a SpeechKit Server, Docker, microphone, local model or cloud key.
The Windows desktop app is the entrypoint for recording a real meeting today.

## Short prompt

```text
Hey AI Agent, go to speechkit.cc and build a small Go Meeting notes demo with synthetic input in this workspace.
```

## Full prompt

```text
Read https://speechkit.cc/llms.txt and the public SpeechKit SDK documentation. Build a small standalone Go Meeting notes demo in this workspace using github.com/kombifyio/SpeechKit/pkg/speechkit/meeting. Read the current examples/meeting/synthetic-host and the package API at the same public release tag before writing code, and pin that version in go.mod. Import public pkg/speechkit packages only.

Use meeting.New with a host-owned PipelineFactory for synthetic microphone and system channels. Have the adapter emit capture events and commit clearly labeled synthetic transcript lines using the existing example's segment lifecycle. Start a session, display its capture snapshots, pause/resume it, stop it, and render the transcript. Build a NotesDocument with a host-written Anchor and source segment references, apply the anchor so its wording is preserved, and export Markdown.

The output and README must say that all transcript and review text is synthetic. This proves runtime composition and notes rendering, not real transcription, audio capture, diarization or AI review generation. Do not open devices, call providers, save raw audio or invent Meeting REST, SSE or TypeScript endpoints. Meeting is a separate Go service beside Dictation, Assist and Voice Agent; do not add a fourth value to those mode enums or to the server one-shot result schema.

Provide go.mod, a runnable main program and a README with one command to run the demo and its expected behavior. Run that command and report the observed lifecycle and Markdown output. State which host adapters, local runtime or provider configuration would be needed for a real recording. If the selected release lacks a required public API, report that limitation instead of fabricating a successful result.
```

## Required artifacts

- `go.mod` with a pinned public SpeechKit version.
- Runnable Go source and a README describing the synthetic inputs.
- A Markdown notes export preserving the supplied note and transcript references.
- The observed local run result, with no claim of provider or device verification.

This SDK starter is separate from the server-backed one-shot verification
matrix. It does not emit `speechkit-one-shot-functional-result.json`.

## References

- https://github.com/kombifyio/SpeechKit/tree/main/examples/meeting/synthetic-host
- https://github.com/kombifyio/SpeechKit/tree/main/pkg/speechkit/meeting
- https://speechkit.cc/modes/meeting
