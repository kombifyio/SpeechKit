# OSS Release Checklist

Use this checklist before syncing to the release repository or cutting a public tag.

- [ ] `LICENSE` exists and matches the intended public license
- [ ] `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `SUPPORT.md`, and `CHANGELOG.md` are present and current
- [ ] no tracked secrets or private credentials are committed
- [ ] no private repo URLs or private org names remain in the public surface
- [ ] no private Doppler defaults are embedded in binaries or scripts
- [ ] no internal-only files such as `AGENTS.md` or `CLAUDE.md` are included in the export
- [ ] `config.example.toml` is OSS-safe and documents only public runtime expectations
- [ ] `frontend/app/README.md` is project-specific and not the stock Vite template
- [ ] architecture docs do not claim unimplemented backends as shipped features
- [ ] GitHub Actions secret `OSS_PUBLISH_TOKEN` or `GITHUB_PAT` in the development repo can read and write `kombifyio/SpeechKit`
- [ ] canonical Windows build succeeds and emits `dist/windows/SpeechKit/` and `dist/windows/SpeechKit-Setup.exe`
- [ ] release artifacts are built from the mirrored public tree
