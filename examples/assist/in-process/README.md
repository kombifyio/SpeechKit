# assist/in-process

Fully in-process Assist: text in, one answer out, no SpeechKit server. The host
owns the LLM call (any OpenAI-compatible `/chat/completions` endpoint) and
`pkg/speechkit/assist.Service` owns the one-shot pipeline, with optional spoken
output through `pkg/speechkit/tts`.

## Run

Local llama.cpp, no cloud key:

```sh
llama-server -m model.gguf --port 8080
SPEECHKIT_LLM_BASE_URL=http://127.0.0.1:8080/v1 go run ./examples/assist/in-process
```

OpenAI (also enables spoken output):

```sh
OPENAI_API_KEY=sk-... go run ./examples/assist/in-process
```

| Variable | Meaning |
| --- | --- |
| `SPEECHKIT_LLM_BASE_URL` | API root of the chat endpoint. Defaults to `https://api.openai.com/v1` when `OPENAI_API_KEY` is set. |
| `SPEECHKIT_LLM_MODEL` | Model name sent in the request. Defaults to `gpt-6-luna`; llama.cpp ignores it. |
| `OPENAI_API_KEY` | OpenAI key. Used for OpenAI TTS output, and for the chat call under the rule below. |
| `SPEECHKIT_LLM_SEND_OPENAI_KEY_TO` | Host name (for example `llm.example.com`) that may receive `OPENAI_API_KEY` as its bearer token. |

## Where requests and the key may go

The example checks the endpoint before it sends anything:

- `SPEECHKIT_LLM_BASE_URL` must be an absolute `https` URL. Plain `http` is
  accepted only for a loopback host (`localhost`, `127.0.0.0/8`, `::1`), so a
  prompt never crosses a network unencrypted. Other host names are not
  resolved to decide this. URLs with credentials, a query string or a fragment
  are refused.
- `OPENAI_API_KEY` is sent as `Authorization: Bearer` only when the endpoint
  host is `api.openai.com` over https. Any other host gets no key, so pointing
  the base URL at a local server or a third-party gateway does not hand it
  your OpenAI key. To send the key to an OpenAI-compatible host on purpose,
  set `SPEECHKIT_LLM_SEND_OPENAI_KEY_TO` to exactly that host name.

The base URL is still operator input, which gosec's SSRF taint analysis (G704)
cannot see past. The two request lines carry a `#nosec G704` that names this
validation as the reason.
