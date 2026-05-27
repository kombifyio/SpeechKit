# Latest Windows Installer

This directory intentionally mirrors the current installable Windows release
inside the repository so a fresh clone can install SpeechKit without browsing
GitHub Actions artifacts first.

Use SpeechKit-Setup.exe for the per-user Windows install. If
SpeechKit-x64.msi is present, it is the per-machine MSI for SCCM/Intune-style
deployment.

The canonical release assets are still the GitHub Release files for $Version
in $SourceRepo. Verify local files with SHA256SUMS.txt or inspect
INSTALLER-MANIFEST.json for source and hash metadata.

Do not put generated installers under dist/; dist/ is local build output.
Only this directory is allowed to carry mirrored installer binaries.
