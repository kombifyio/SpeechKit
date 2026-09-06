# Wake-word robustness: analysis and improvement proposals

Basis: SpeechKit `main` (v0.48 line), `pkg/speechkit/wakeword`,
`pkg/speechkit/wakeword/sherpa`, the Windows client sidecars
(`speechkit-wakeword.exe` KWS / `speechkit-openwakeword.exe`).
Trigger: wake words do not yet fire reliably in the current framework.

## Findings in the framework code (concrete)

1. **`MinConsecutiveFrames` is overwritten (bug).**
   `pipeline_cgo.go` -> `normalizeConfig()` unconditionally sets
   `cfg.MinConsecutiveFrames = defaultMinConsecutiveFrames` (=1).
   Host configuration has no effect; the debounce logic below it
   (the consecutiveHits map) is therefore dead code. Either respect the
   field or remove field and map.

2. **`DetectionEvent.Probability` is hard-coded to `1.0`.**
   `sherpa GetResult` delivers no score to the host. Without a score nobody
   can tune thresholds from data. Proposal: put token scores from sherpa
   (where available) or at least the threshold configuration in use into
   the event; additionally a `PartialEvent`/debug hook ("almost fired") for
   calibration UIs.

3. **Inline `Keywords` are NOT tokenized (silent malfunction).**
   `DetectorConfig.Keywords` writes raw text ("kombify") into a temp file.
   The KWS model, however, expects BPE tokens (`▁COM B IF Y :2.0 @kombify`).
   Raw text never matches -> the wake word simply does not fire, without an
   error. Exactly this format trap has happened before (CHANGELOG 0.48.x:
   "keywords.txt was written in a format sherpa-onnx could not parse").
   **Fix with the biggest leverage:** `wakeword.EncodeKeywords()` in the
   framework: loads `tokens.txt` + `bpe.model` from the model directory and
   tokenizes plain-text keywords at startup (greedy longest match on
   tokens.txt is enough for BPE-500; alternatively sherpa text2token via
   cgo). Plus **startup validation** of the keywords file against tokens.txt
   with a clear error message instead of a silent failure.

4. **No audio front-end contract.**
   `FeedPCM` demands 16 kHz S16LE mono but only checks alignment. The most
   common real cause of "detects nothing": the host feeds 44.1/48 kHz or
   stereo. Proposal: a `wakeword.Frontend` helper (resampler to 16k, stereo
   downmix, optional high-pass + simple AGC) plus docs, or at least a format
   heuristic with a warning (energy in the Nyquist band).

5. **No echo/barge-in protection.**
   During TTS output the detection keeps running and triggers on its own
   voice (especially when the Box microphone sits next to the Box speaker).
   Proposal: a `Pipeline.Pause()/Resume()` API plus a companion example that
   pauses during playback; later an AEC hook.

6. **Model discovery is fragile.**
   Encoder/decoder/joiner file names must be given exactly. Proposal:
   `sherpa.NewDetectorFromDir(dir, opts)` with globbing (`encoder-*.onnx`
   etc.) plus stricter error messages (which file is missing).

7. **Threshold semantics are duplicated and undocumented.**
   Global `KeywordsThreshold` versus per-keyword `#0.x` in the file versus
   `:boost`. docs/wakeword.md explains none of them. A short "tuning table"
   (threshold down = more sensitive, boost up = prefer the phrase) saves
   users hours.

## Windows client (UX proposals)

- **Calibration assistant**: record the phrase 3x and counter-examples 3x,
  threshold sweep, result as a score plot; saving writes config + keywords
  atomically.
- **Live telemetry in the settings panel**: input level meter of the selected
  device, "last 10 detections/near detections" with scores, sidecar health
  (running/crash/restart counter).
- **Keyword lint on save**: format and token check against the bundled
  model, errors shown inline (would have caught the 0.48 bug).
- **Test button**: send bundled reference WAVs ("Hey Quby" etc.) through the
  spotter -> immediate pass/fail independent of the microphone.
- **Device choice per feature**: wake-word microphone separate from the
  dictation microphone (satellite scenario, kombify box!).

## Backend strategy

- `livekit_openwakeword` (default, per-phrase ONNX): good for "Hey X"
  phrases, but every new word needs training. Measure the quality of the
  bundled models (FA/FR rate on a fixed test set) and track it in the repo.
- `sherpa` KWS (text-defined keywords): flexible (kombify definable at once),
  but more sensitive to threshold/format. With fixes 1/3/4 it becomes the
  robust default for brand wake words.
- Recommendation: **ensemble option**: oww for the curated phrases, sherpa
  for custom/brand keywords, both behind the same `wakeword.Sink`.

## Test infrastructure

- Golden WAV suite (`testdata/`): per phrase 5 positives (different
  speakers/distances) + 10 negatives; a CI job runs the spotter and asserts
  FA=0, FR<=1. Prevents regressions like the keywords format bug.
- Bench: detection latency (frame feed -> event) as a measured value.

## Priority (effort/benefit)

| # | Measure | Effort | Benefit |
|---|---------|--------|---------|
| 1 | Inline keyword tokenization + startup validation | S | very high |
| 2 | Fix the MinConsecutiveFrames bug | XS | high |
| 3 | Pause/Resume (echo protection) | S | high |
| 4 | Front-end helper (resample/downmix) | M | high |
| 5 | Scores in events + calibration UI | M | medium-high |
| 6 | NewDetectorFromDir + error messages | S | medium |
| 7 | Golden WAV CI | M | medium (high long-term) |
