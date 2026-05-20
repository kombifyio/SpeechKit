!include "MUI2.nsh"
!include "LogicLib.nsh"

; --- General ---
Name "kombify SpeechKit"
!define STAGE_DIR "..\dist\windows\SpeechKit"
OutFile "..\dist\windows\SpeechKit-Setup.exe"
InstallDir "$LOCALAPPDATA\SpeechKit"
RequestExecutionLevel user

; VERSION can be overridden at compile time: makensis /DVERSION=x.y.z
!ifndef VERSION
  !define VERSION "0.35.8"
!endif

; --- Interface ---
!define MUI_ICON "speechkit.ico"
!define MUI_ABORTWARNING

; --- Pages ---
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"

; --- Install Section ---
Section "SpeechKit" SecMain
  SetOutPath "$INSTDIR"

  ; Main binary
  File "${STAGE_DIR}\SpeechKit.exe"
  File "${STAGE_DIR}\whisper-server.exe"
  File "${STAGE_DIR}\speechkit-wakeword.exe"
  File "${STAGE_DIR}\*.dll"
  File "${STAGE_DIR}\MicrosoftEdgeWebview2Setup.exe"

  ; Bundled starter Whisper model (ggml-small.bin + tiny). Without this
  ; the first-launch /app/complete-setup gate returns 409 (no local
  ; speech model configured) and onboarding stalls until the user
  ; downloads a model. The build script verifies the SHA256 of
  ; ggml-small.bin before packaging — see scripts/build.ps1 and
  ; scripts/prepare-whisper-runtime.ps1.
  SetOutPath "$INSTDIR\models"
  File /r "${STAGE_DIR}\models\*"
  SetOutPath "$INSTDIR"

  ; Local LLM runtime (llama-server.exe + its private DLLs). Without
  ; this, the bundled SpeechKit cannot run the local LLM path and
  ; Assist/Voice-Agent fall back to a hard "missing_model" error.
  ; The /r switch recurses into the llama/ subdirectory.
  SetOutPath "$INSTDIR\llama"
  File /r "${STAGE_DIR}\llama\*"
  SetOutPath "$INSTDIR"

  ; Wake-word KWS model bundle (sherpa-onnx Zipformer encoder/decoder/
  ; joiner ONNX + keywords + tokens). The sidecar binary
  ; speechkit-wakeword.exe (staged with the main binaries above) loads
  ; these from $INSTDIR\wakeword-kws relative to its own executable.
  SetOutPath "$INSTDIR\wakeword-kws"
  File /r "${STAGE_DIR}\wakeword-kws\*"
  SetOutPath "$INSTDIR"

  ; Runtime config template
  File "/oname=config.default.toml" "${STAGE_DIR}\config.toml"

  ; Create default config if not exists
  IfFileExists "$INSTDIR\config.toml" +2
    CopyFiles "$INSTDIR\config.default.toml" "$INSTDIR\config.toml"

  ; Ensure WebView2 runtime for Wails UI
  Call EnsureWebView2Runtime

  SetOutPath "$INSTDIR"

  ; Create Start Menu shortcuts
  CreateDirectory "$SMPROGRAMS\kombify SpeechKit"
  CreateShortcut "$SMPROGRAMS\kombify SpeechKit\SpeechKit.lnk" "$INSTDIR\SpeechKit.exe"
  CreateShortcut "$SMPROGRAMS\kombify SpeechKit\Uninstall.lnk" "$INSTDIR\uninstall.exe"

  ; Uninstaller
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Add/Remove Programs entry
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "DisplayName" "kombify SpeechKit"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "Publisher" "kombify"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "DisplayVersion" "${VERSION}"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "NoRepair" 1

SectionEnd

; --- Uninstall Section ---
Section "Uninstall"
  ; Remove Add/Remove Programs entry
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit"
  ; Remove files
  Delete "$INSTDIR\SpeechKit.exe"
  Delete "$INSTDIR\whisper-server.exe"
  Delete "$INSTDIR\speechkit-wakeword.exe"
  Delete "$INSTDIR\*.dll"
  Delete "$INSTDIR\MicrosoftEdgeWebview2Setup.exe"
  Delete "$INSTDIR\config.toml"
  Delete "$INSTDIR\config.default.toml"
  Delete "$INSTDIR\uninstall.exe"
  RMDir /r "$INSTDIR\models"
  RMDir /r "$INSTDIR\llama"
  RMDir /r "$INSTDIR\wakeword-kws"

  ; Remove shortcuts
  Delete "$SMPROGRAMS\kombify SpeechKit\SpeechKit.lnk"
  Delete "$SMPROGRAMS\kombify SpeechKit\Uninstall.lnk"
  RMDir "$SMPROGRAMS\kombify SpeechKit"

  ; Remove install dir (only if empty or user confirms)
  RMDir "$INSTDIR"
SectionEnd

Function IsWebView2RuntimeInstalled
  ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKLM "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}

  ${If} $0 == ""
    Push "0"
  ${Else}
    Push "1"
  ${EndIf}
FunctionEnd

Function EnsureWebView2Runtime
  Call IsWebView2RuntimeInstalled
  Pop $0
  ${If} $0 == "0"
    DetailPrint "Installing Microsoft Edge WebView2 Runtime..."
    ExecWait '"$INSTDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $1
    ${If} $1 != 0
      MessageBox MB_ICONEXCLAMATION|MB_OK "WebView2 runtime could not be installed automatically. SpeechKit may require an internet connection on first launch."
    ${EndIf}
  ${EndIf}
FunctionEnd
