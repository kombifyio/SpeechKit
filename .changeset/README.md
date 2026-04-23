# Changesets

SpeechKit uses Changesets to track release intent for the shared source surface and the Windows host release line.

Use `npm run changeset` to add a release note and `npm run version` to apply the version bump plus the default sync for:

- root package metadata and lockfile
- frontend package metadata
- Windows host metadata and installer version

Surfaces outside the current public OSS export are versioned explicitly and are not moved by the default release flow.
