// Package flowadapt adapts the generation flows of app/internal/ai/flows to
// the public Assist and Voice Agent contracts (assist.Generator,
// cascaded.Agent). It is separate from flows so a host that links the flows
// for summaries and meeting notes does not link Assist, the Voice Agent or
// text-to-speech.
package flowadapt
