Unicode true
RequestExecutionLevel user
Name "LanRAG Lite"
OutFile "${INSTALLER_OUT}"
InstallDir "$LOCALAPPDATA\Programs\LanRAG Lite"
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\LanRAG Lite" "InstallLocation"
ShowInstDetails show

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "LanRAG Lite requires Windows x64."
    Abort
  ${EndIf}
FunctionEnd

Section
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  File "${PAYLOAD_DIR}\LanRAG Lite.exe"
  File "${PAYLOAD_DIR}\LanRAG Desktop.exe"
  File "${PAYLOAD_DIR}\extension-host.exe"
  File "${PAYLOAD_DIR}\model-supervisor.exe"
  File "${PAYLOAD_DIR}\LICENSE"
  File "${PAYLOAD_DIR}\THIRD_PARTY_NOTICES.md"
  File /r "${PAYLOAD_DIR}\resources"
  File /r "${PAYLOAD_DIR}\licenses"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\LanRAG Lite" "DisplayName" "LanRAG Lite"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\LanRAG Lite" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\LanRAG Lite" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  CreateShortcut "$SMPROGRAMS\LanRAG Lite.lnk" "$INSTDIR\LanRAG Lite.exe"
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  Delete "$SMPROGRAMS\LanRAG Lite.lnk"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\LanRAG Lite"
  Delete "$INSTDIR\LanRAG Lite.exe"
  Delete "$INSTDIR\LanRAG Desktop.exe"
  Delete "$INSTDIR\extension-host.exe"
  Delete "$INSTDIR\model-supervisor.exe"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.md"
  RMDir /r "$INSTDIR\resources"
  RMDir /r "$INSTDIR\licenses"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
SectionEnd
