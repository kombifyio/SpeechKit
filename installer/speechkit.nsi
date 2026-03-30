!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "nsDialogs.nsh"

; --- General ---
Name "kombify SpeechKit"
!define STAGE_DIR "..\dist\windows\SpeechKit"
OutFile "..\dist\windows\SpeechKit-Setup.exe"
InstallDir "$LOCALAPPDATA\kombify\SpeechKit"
RequestExecutionLevel user

; --- Interface ---
!define MUI_ICON "speechkit.ico"
!define MUI_ABORTWARNING

Var HFTokenDialog
Var HFTokenInput
Var HFTokenValue

; --- Pages ---
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
Page custom HFTokenPageCreate HFTokenPageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "English"

Function HFTokenPageCreate
  nsDialogs::Create 1018
  Pop $HFTokenDialog
  ${If} $HFTokenDialog == error
    Abort
  ${EndIf}

  ${NSD_CreateLabel} 0 0 100% 24u "Optional: Enter a default Hugging Face token for this installation. It will be migrated into secure local storage on first app start."
  Pop $0
  ${NSD_CreatePassword} 0 30u 100% 12u ""
  Pop $HFTokenInput

  nsDialogs::Show
FunctionEnd

Function HFTokenPageLeave
  ${NSD_GetText} $HFTokenInput $HFTokenValue
FunctionEnd

; --- Install Section ---
Section "SpeechKit" SecMain
  SetOutPath "$INSTDIR"

  ; Main binary
  File "${STAGE_DIR}\SpeechKit.exe"

  ; Runtime config template
  File "/oname=config.default.toml" "${STAGE_DIR}\config.toml"

  ; Create default config if not exists
  IfFileExists "$INSTDIR\config.toml" +2
    CopyFiles "$INSTDIR\config.default.toml" "$INSTDIR\config.toml"

  ; Create models directory
  CreateDirectory "$INSTDIR\models"

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
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "DisplayVersion" "0.1.3"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit" "NoRepair" 1

  ${If} $HFTokenValue != ""
    WriteRegStr HKCU "Software\kombify\SpeechKit" "PendingHFInstallToken" "$HFTokenValue"
  ${EndIf}
SectionEnd

; --- Uninstall Section ---
Section "Uninstall"
  ; Remove Add/Remove Programs entry
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\kombify SpeechKit"
  DeleteRegValue HKCU "Software\kombify\SpeechKit" "PendingHFInstallToken"

  ; Remove files
  Delete "$INSTDIR\SpeechKit.exe"
  Delete "$INSTDIR\config.toml"
  Delete "$INSTDIR\config.default.toml"
  Delete "$INSTDIR\uninstall.exe"

  ; Remove shortcuts
  Delete "$SMPROGRAMS\kombify SpeechKit\SpeechKit.lnk"
  Delete "$SMPROGRAMS\kombify SpeechKit\Uninstall.lnk"
  RMDir "$SMPROGRAMS\kombify SpeechKit"

  ; Remove install dir (only if empty or user confirms)
  RMDir "$INSTDIR"
SectionEnd
