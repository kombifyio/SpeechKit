# OSS Release Checklist

Use this checklist before syncing to the release repository or cutting a public tag.

- [ ] selected release surfaces are explicit for this run: source-only, Windows portable, Windows installer, Android
- [ ] `LICENSE` exists and matches the intended public license
- [ ] `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `SUPPORT.md`, and `CHANGELOG.md` are present and current
- [ ] no tracked secrets or private credentials are committed
- [ ] no private repo URLs or private org names remain in the public surface
- [ ] no private Doppler defaults are embedded in binaries or scripts
- [ ] no internal-only files such as `AGENTS.md` or `CLAUDE.md` are included in the export
- [ ] `config.example.toml` is OSS-safe and documents only public runtime expectations
- [ ] `frontend/app/README.md` is project-specific and not the stock Vite template
- [ ] architecture docs do not claim unimplemented backends as shipped features
- [ ] `RELEASE_APP_ID` and `RELEASE_APP_PRIVATE_KEY` are configured at a scope the source workflow can actually read; for private repos on GitHub Free that means repo scope, not org scope
- [ ] `OSS_PUBLISH_TOKEN` exists only if the GitHub App bootstrap is still in progress and has not been fully cut over yet
- [ ] canonical Windows build succeeds and emits `dist/windows/SpeechKit/` and `dist/windows/SpeechKit-Setup.exe`
- [ ] `dist/windows/SpeechKit/SpeechKit.exe` and `dist/windows/SpeechKit-Setup.exe` pass `pwsh ./scripts/validate-windows-signing.ps1 -RequireInstaller -RequireTimestamp -ExpectedPublisher '<publisher>'`
- [ ] if the release is source-only, Windows artifacts are explicitly disabled in the release workflow
- [ ] if the release includes a Windows installer, installer smoke checks run on a clean Windows VM or Windows runner
- [ ] if Android is part of the release, `npm run version:sync:android` was run intentionally and Android artifacts were built separately
- [ ] release artifacts are built from the mirrored public tree
