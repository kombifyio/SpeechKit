// Package procguard ties long-lived child processes to the lifetime of the
// process that spawned them.
//
// SpeechKit's CPU-heavy children — the wake-word sidecars, whisper-server and
// the local LLM server — are deliberately rooted in a background context so a
// short-lived HTTP request context cannot kill them (see
// cmd/speechkit/desktop_wakeword.go for that bug history). Their shutdown is
// owned by the host's own Close paths and the startup cleanup stack.
//
// That covers every ORDERLY exit. It does not cover a crash, a taskkill, or a
// dev-loop rebuild that replaces the host binary: those skip the cleanup stack
// entirely and leave the children running forever. Each orphan holds on the
// order of a gigabyte of commit charge, so a day of restarts can exhaust the
// system commit limit while physical memory still looks healthy — at which
// point unrelated tools start failing to allocate (observed 2026-08-27:
// 6 orphans, ~7 GB, commit limit down to 0.4 GB free of 47.9 GB).
//
// Adopt closes that gap by handing the child to the operating system: the OS
// terminates it when the parent goes away, however the parent goes away.
package procguard
