```bash
curl -H "Authorization: Bearer $SPEECHKIT_TOKEN" \
  -F audio=@hello.wav \
  -F language=en \
  "$SPEECHKIT_SERVER_URL/v1/dictation/transcribe"
```

Use the canonical `/v1/*` path. `/api/v1/*` remains an alias for deployment
compatibility.
