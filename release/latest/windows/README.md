# Latest Windows Installer

This directory intentionally mirrors metadata for the current installable
Windows release. Installer binaries are kept as GitHub Release assets, not as
tracked source files.

Use INSTALLER-MANIFEST.json to find the canonical download URL, size, and
SHA-256 for SpeechKit-Setup.exe and SpeechKit-x64.msi.

The canonical release assets are still the GitHub Release files for $Version
in $SourceRepo. Verify local files with SHA256SUMS.txt or inspect
INSTALLER-MANIFEST.json for source and hash metadata.

Do not commit generated installers under release/latest/windows or dist/; both
are local/generated artifact locations.
