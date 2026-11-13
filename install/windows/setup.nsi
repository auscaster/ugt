; Windows presentation of the canonical installer, not another runtime setup.
; makensis -DVERSION=0.3.3 -DOUTPUT=/absolute/path/ugt-setup-windows.exe setup.nsi
Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!ifndef VERSION
  !error "Pass VERSION for the helper release this installer installs"
!endif
!ifndef OUTPUT
  !error "Pass OUTPUT for the installer file"
!endif
!ifndef SOURCE_ROOT
  !error "Pass SOURCE_ROOT for the repository root"
!endif

Name "Use the Geek Thing"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\UGT\bin"
RequestExecutionLevel user
SetCompressor /SOLID lzma
ShowInstDetails hide
ShowUninstDetails hide
BrandingText "Use the Geek Thing"
Var SetupCode
Var FinishTitle
Var FinishText

!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Get GitHub projects working"
!define MUI_WELCOMEPAGE_TEXT "Setup installs UGT for your Windows account and connects the AI apps it finds.$\r$\n$\r$\nUGT uses a Linux environment to run projects. If Windows needs WSL, it will ask for administrator permission and may need a restart.$\r$\n$\r$\nYour saved project stays in your browser."
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_TITLE "$FinishTitle"
!define MUI_FINISHPAGE_TEXT "$FinishText"
!define MUI_FINISHPAGE_TEXT_LARGE
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!define MUI_FINISHPAGE_RUN_TEXT "Open Use the Geek Thing"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenWebsite
!define MUI_PAGE_CUSTOMFUNCTION_SHOW FinishShow
!insertmacro MUI_PAGE_FINISH
!define MUI_UNCONFIRMPAGE_TEXT_TOP "This removes UGT's AI-app connections, its environment, installed projects, saved settings and secrets.$\r$\n$\r$\nYour result files and recipes you wrote are kept. Close your AI apps before continuing."
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=install.ps1 "${SOURCE_ROOT}\install\install.ps1"
  System::Call 'kernel32::SetEnvironmentVariable(t "UGT_INSTALL_DIR", t "$INSTDIR") i .r0'
  System::Call 'kernel32::SetEnvironmentVariable(t "UGT_VERSION", t "${VERSION}") i .r0'
  DetailPrint "Downloading UGT and preparing this computer. This may take a few minutes."
  nsExec::ExecToLog /TIMEOUT=1200000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\install.ps1" -Installer'
  Pop $SetupCode
  ; Even interrupted setup needs a visible retry and removal route.
  IfFileExists "$INSTDIR\ugt.exe" 0 +2
    Call RegisterInstallation
  ${If} $SetupCode == 3010
    SetRebootFlag true
    StrCpy $FinishTitle "Restart Windows to continue"
    StrCpy $FinishText "Restart Windows, then open Finish UGT Setup from Start.$\r$\n$\r$\nYour project stays saved in your browser."
    SetErrorLevel 3010
  ${ElseIf} $SetupCode == 0
    StrCpy $FinishTitle "UGT is ready"
    StrCpy $FinishText "Return to your project in your browser and choose Open in UGT.$\r$\n$\r$\nOr start a new chat in your AI app and ask for the project."
  ${Else}
    DetailPrint "Setup could not finish. Read the details above, then reopen UGT Setup to retry."
    SetErrorLevel 1
    Abort "Setup needs attention. Reopen UGT Setup to retry."
  ${EndIf}
SectionEnd

Function RegisterInstallation
  SetOutPath "$INSTDIR"
  File /oname=LICENSE.txt "${SOURCE_ROOT}\LICENSE"
  StrCmp "$EXEPATH" "$INSTDIR\Setup.exe" +2
    CopyFiles /SILENT "$EXEPATH" "$INSTDIR\Setup.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\Use the Geek Thing"
  ${If} $SetupCode == 0
    Delete "$SMPROGRAMS\Use the Geek Thing\Finish UGT Setup.lnk"
    WriteINIStr "$SMPROGRAMS\Use the Geek Thing\Use the Geek Thing.url" "InternetShortcut" "URL" "https://usegeekthing.com/"
  ${Else}
    Delete "$SMPROGRAMS\Use the Geek Thing\Use the Geek Thing.url"
    CreateShortcut "$SMPROGRAMS\Use the Geek Thing\Finish UGT Setup.lnk" "$INSTDIR\Setup.exe"
  ${EndIf}
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "DisplayName" "Use the Geek Thing"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "Publisher" "Use the Geek Thing"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "URLInfoAbout" "https://usegeekthing.com"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing" "NoRepair" 1
FunctionEnd

Function OpenWebsite
  ${If} $SetupCode == 0
    ExecShell "open" "https://usegeekthing.com/"
  ${EndIf}
FunctionEnd

Function FinishShow
  ${If} $SetupCode == 3010
    ShowWindow $mui.FinishPage.Run 0
  ${EndIf}
FunctionEnd

Section "Uninstall"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=install.ps1 "${SOURCE_ROOT}\install\install.ps1"
  System::Call 'kernel32::SetEnvironmentVariable(t "UGT_INSTALL_DIR", t "$INSTDIR") i .r0'
  nsExec::ExecToLog /TIMEOUT=1200000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\install.ps1" -Installer -Uninstall'
  Pop $SetupCode
  ${If} $SetupCode != 0
    SetErrorLevel 1
    Abort "UGT could not finish removal. Close your AI apps, check the details and retry."
  ${EndIf}
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\Setup.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Use the Geek Thing\Finish UGT Setup.lnk"
  Delete "$SMPROGRAMS\Use the Geek Thing\Use the Geek Thing.url"
  RMDir "$SMPROGRAMS\Use the Geek Thing"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\UseTheGeekThing"
SectionEnd
