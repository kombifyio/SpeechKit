# Meeting synthetic host

This example embeds the public `pkg/speechkit/meeting` runtime in a plain Go
host. It uses an in-memory adapter that emits public capture events and commits
synthetic transcript lines, so it does not open a microphone, call an STT
provider, or require cloud credentials.

## Run

From the repository root:

```bash
go run ./examples/meeting/synthetic-host
```

The output shows the runtime state transition, the synthetic transcript rendered
through Meeting helpers, and a small notes document that preserves a host-owned
anchor.
