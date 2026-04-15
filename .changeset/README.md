# Changesets

SpeechKit uses Changesets to track release intent for the shared source surface and the Windows host release line.

Use `npm run changeset` to add a release note and `npm run version` to apply the version bump plus the default sync for:

- root package metadata and lockfile
- frontend package metadata
- Windows host metadata and installer version

Android versioning is explicit on purpose. Only run `npm run version:sync:android` when the Android release surface should move as part of the current release.
