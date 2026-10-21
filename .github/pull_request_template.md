## Summary

- what changed
- why it changed

## Verification

- [ ] `powershell -ExecutionPolicy Bypass -File scripts/build.ps1`
- [ ] proven end to end: the affected journey ran as a shared test user against the running product
- [ ] no new unit tests outside the boundaries in `.kombify/test-boundaries.json`
- [ ] docs updated when needed

## Release Boundary Check

- [ ] no secrets added
- [ ] no private repo references added
- [ ] no internal-only files exposed

## Notes

- risks
- follow-up work
