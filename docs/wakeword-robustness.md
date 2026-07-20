# Wakeword-Robustheit: Analyse & Verbesserungsvorschlaege

Basis: SpeechKit `main` (v0.48-Linie), `pkg/speechkit/wakeword`,
`pkg/speechkit/wakeword/sherpa`, Windows-Client-Sidecars
(`speechkit-wakeword.exe` KWS / `speechkit-openwakeword.exe`).
Anlass: Wakewords feuern im aktuellen Framework noch nicht zuverlaessig.

## Befunde im Framework-Code (konkret)

1. **`MinConsecutiveFrames` wird ueberschrieben (Bug).**
   `pipeline_cgo.go` -> `normalizeConfig()` setzt unconditionally
   `cfg.MinConsecutiveFrames = defaultMinConsecutiveFrames` (=1).
   Host-Konfiguration ist wirkungslos; die Debounce-Logik darunter
   (consecutiveHits-Map) ist damit toter Code. Entweder Feld respektieren
   oder Feld + Map entfernen.

2. **`DetectionEvent.Probability` ist hart `1.0`.**
   `sherpa GetResult` liefert keinen Score an den Host. Ohne Score kann
   niemand Thresholds datenbasiert tunen. Vorschlag: Token-Scores aus
   sherpa (falls verfuegbar) bzw. mindestens die verwendete
   Threshold-Konfiguration ins Event legen; zusaetzlich ein
   `PartialEvent`/Debug-Hook ("fast gefeuert") fuer Kalibrier-UIs.

3. **Inline-`Keywords` werden NICHT tokenisiert (stille Fehlfunktion).**
   `DetectorConfig.Keywords` schreibt Rohtext ("kombify") in eine
   Temp-Datei. Das KWS-Modell erwartet aber BPE-Tokens
   (`▁COM B IF Y :2.0 @kombify`). Rohtext matcht nie -> Wakeword feuert
   einfach nicht, ohne Fehler. Genau diese Formatfalle gab es schon einmal
   (CHANGELOG 0.48.x: "keywords.txt was written in a format sherpa-onnx
   could not parse").
   **Fix mit dem groessten Hebel:** `wakeword.EncodeKeywords()` im
   Framework - laedt `tokens.txt` + `bpe.model` aus dem ModelDir und
   tokenisiert Klartext-Keywords beim Start (greedy longest-match auf
   tokens.txt reicht fuer BPE-500; alternativ sherpa text2token via cgo).
   Dazu: **Startup-Validierung** der keywords-Datei gegen tokens.txt mit
   klarer Fehlermeldung statt silent fail.

4. **Kein Audio-Frontend-Vertrag.**
   `FeedPCM` verlangt 16 kHz S16LE mono, prueft aber nur Alignment.
   Haeufigste reale Ursache fuer "erkennt nichts": Host fuettert 44.1/48 kHz
   oder Stereo. Vorschlag: `wakeword.Frontend`-Helper (Resampler auf 16k,
   Stereo-Downmix, optional Hochpass + einfache AGC) + Doku, oder
   zumindest Format-Heuristik mit Warnung (Energie in Nyquist-Band).

5. **Kein Echo-/Barge-in-Schutz.**
   Waehrend TTS-Ausgabe laeuft die Erkennung weiter und triggert auf die
   eigene Stimme (v.a. wenn Box-Mikro neben Box-Speaker sitzt).
   Vorschlag: `Pipeline.Pause()/Resume()` API + companion-Beispiel, das
   waehrend Playback pausiert; spaeter AEC-Hook.

6. **Modell-Discovery ist fragil.**
   Encoder/Decoder/Joiner-Dateinamen muessen exakt angegeben werden.
   Vorschlag: `sherpa.NewDetectorFromDir(dir, opts)` mit Glob
   (`encoder-*.onnx` etc.) + haerteren Fehlermeldungen (welche Datei fehlt).

7. **Threshold-Semantik doppelt & undokumentiert.**
   Global `KeywordsThreshold` vs. per-Keyword `#0.x` in der Datei vs.
   `:boost`. docs/wakeword.md erklaert keins davon. Eine kurze
   "Tuning-Tabelle" (Threshold runter = sensibler, Boost hoch = Phrase
   bevorzugen) spart Anwendern Stunden.

## Windows-Client (UX-Vorschlaege)

- **Kalibrier-Assistent**: 3x Phrase aufnehmen, 3x Gegenbeispiele,
  Threshold-Sweep, Ergebnis als Score-Plot; Speichern schreibt config +
  keywords atomar.
- **Live-Telemetrie im Settings-Panel**: Input-Pegelmeter des gewaehlten
  Geraets, "letzte 10 Detektionen/Fast-Detektionen" mit Scores,
  Sidecar-Health (laeuft/crash/Neustartzaehler).
- **Keyword-Lint beim Speichern**: Format- und Token-Pruefung gegen das
  gebundelte Modell, Fehler inline anzeigen (haette den 0.48-Bug abgefangen).
- **Test-Button**: gebundelte Referenz-WAVs ("Hey Quby" etc.) durch den
  Spotter schicken -> sofortiges Pass/Fail unabhaengig vom Mikrofon.
- **Geraetewahl pro Feature**: Wakeword-Mikro getrennt vom Dictation-Mikro
  (Satelliten-Szenario kombify box!).

## Backend-Strategie

- `livekit_openwakeword` (Default, per-Phrase-ONNX): gut fuer "Hey X"-
  Phrasen, aber jedes neue Wort braucht Training. Qualitaet der gebundelten
  Modelle messen (FA/FR-Rate auf festem Testset) und im Repo tracken.
- `sherpa` KWS (text-definierte Keywords): flexibel (kombify sofort
  definierbar), dafuer empfindlicher bei Threshold/Format. Mit Fixes 1/3/4
  wird das der robuste Default fuer Brand-Wakewords.
- Empfehlung: **Ensemble-Option** - oww fuer die kuratierten Phrasen,
  sherpa fuer Custom/Brand-Keywords, beide hinter demselben
  `wakeword.Sink`.

## Test-Infrastruktur

- Golden-WAV-Suite (`testdata/`): je Phrase 5 Positive (verschiedene
  Sprecher/Abstaende) + 10 Negative; CI-Job laesst den Spotter laufen und
  asserted FA=0, FR<=1. Verhindert Regressionen wie den keywords-Format-Bug.
- Bench: Erkennungslatenz (Frame-Einspeisung -> Event) als Messwert.

## Prioritaet (Aufwand/Nutzen)

| # | Massnahme | Aufwand | Nutzen |
|---|-----------|---------|--------|
| 1 | Inline-Keyword-Tokenisierung + Startup-Validierung | S | sehr hoch |
| 2 | MinConsecutiveFrames-Bug fixen | XS | hoch |
| 3 | Pause/Resume (Echo-Schutz) | S | hoch |
| 4 | Frontend-Helper (Resample/Downmix) | M | hoch |
| 5 | Scores in Events + Kalibrier-UI | M | mittel-hoch |
| 6 | NewDetectorFromDir + Fehlermeldungen | S | mittel |
| 7 | Golden-WAV-CI | M | mittel (langfristig hoch) |
