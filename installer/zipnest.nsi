; ZipNest Windows installer (NSIS 3, Modern UI 2).
; Build:  makensis -DVERSION=1.0.0 -DSRCDIR=<folder with ZipNest.exe> -DOUTFILE=<setup.exe> zipnest.nsi

Target amd64-unicode   ; native 64-bit installer (ZipNest itself is 64-bit)
SetCompressor /SOLID lzma

!ifndef VERSION
  !define VERSION "2.0.0"
!endif
!ifndef SRCDIR
  !define SRCDIR "../dist/ZipNest-${VERSION}-windows-amd64"
!endif
!ifndef OUTFILE
  !define OUTFILE "../dist/ZipNest-${VERSION}-setup.exe"
!endif

!define APP "ZipNest"
!define UNKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP}"

Name "${APP} ${VERSION}"
Caption "${APP} ${VERSION} Setup"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\${APP}"
InstallDirRegKey HKLM "Software\${APP}" "InstallDir"
RequestExecutionLevel admin
BrandingText "${APP} ${VERSION}"
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APP}"
VIAddVersionKey "FileDescription" "${APP} Setup"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "${APP} contributors"

!include "MUI2.nsh"
!include "x64.nsh"
!include "LogicLib.nsh"

; ---------------------------------------------------------------- look
!define MUI_ICON "../cmd/zipnest/res/zipnest.ico"
!define MUI_UNICON "../cmd/zipnest/res/zipnest.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_RIGHT
!define MUI_HEADERIMAGE_BITMAP "header.bmp"
!define MUI_HEADERIMAGE_UNBITMAP "header.bmp"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_SMALLDESC

; ---------------------------------------------------------------- pages
!define MUI_WELCOMEPAGE_TITLE "Welcome to ${APP} ${VERSION} Setup"
!define MUI_WELCOMEPAGE_TEXT "${APP} is a free archive manager.$\r$\n$\r$\n\
  Open and extract ZIP, RAR, 7z, TAR, GZ, BZ2, XZ and ZST archives, and create \
  ZIP archives protected with AES-256 passwords.$\r$\n$\r$\n\
  Click Next to continue."
!insertmacro MUI_PAGE_WELCOME

!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_INSTFILES

!define MUI_FINISHPAGE_TITLE "${APP} has been installed"
!define MUI_FINISHPAGE_TEXT "${APP} is ready to use.$\r$\n$\r$\n\
  Double-click any archive, or right-click files in File Explorer and open the \
  ${APP} menu. You can change file types and menu options at any time in \
  ${APP} under Options > Settings."
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Run ${APP} now"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApp
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------- install
Function .onInit
  ${IfNot} ${RunningX64}
  ${AndIfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "${APP} requires 64-bit Windows."
    Abort
  ${EndIf}
  SetRegView 64
FunctionEnd

Function LaunchApp
  ; Start through Explorer so the app does not run with the installer's admin rights.
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\ZipNest.exe"'
FunctionEnd

!ifndef CERT_SHA1
  !define CERT_SHA1 ""
!endif

Var WinBuild

Section "Install"
  SetOutPath "$INSTDIR"
  ReadRegStr $WinBuild HKLM "SOFTWARE\Microsoft\Windows NT\CurrentVersion" "CurrentBuildNumber"

  ; Explorer may still have the previous menu DLL loaded: move it aside first.
  IfFileExists "$INSTDIR\ZipNestShell.dll" 0 +4
    Delete "$INSTDIR\ZipNestShell.dll.old"
    Rename "$INSTDIR\ZipNestShell.dll" "$INSTDIR\ZipNestShell.dll.old"
    Delete /REBOOTOK "$INSTDIR\ZipNestShell.dll.old"

  File "${SRCDIR}/ZipNest.exe"
  File "${SRCDIR}/zipnest-cli.exe"
  File "${SRCDIR}/ZipNestShell.dll"
  File "${SRCDIR}/ZipNestShell.msix"
  File "${SRCDIR}/ZipNest.cer"
  File "${SRCDIR}/README.md"
  File "${SRCDIR}/THIRD_PARTY_NOTICES.md"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  WriteRegStr HKLM "Software\${APP}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\App Paths\ZipNest.exe" "" "$INSTDIR\ZipNest.exe"

  ; Entry in Settings > Apps / Control Panel > Programs and Features.
  WriteRegStr HKLM "${UNKEY}" "DisplayName" "${APP} ${VERSION}"
  WriteRegStr HKLM "${UNKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNKEY}" "Publisher" "${APP}"
  WriteRegStr HKLM "${UNKEY}" "DisplayIcon" "$INSTDIR\ZipNest.exe,0"
  WriteRegStr HKLM "${UNKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${UNKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNKEY}" "NoRepair" 1
  WriteRegDWORD HKLM "${UNKEY}" "EstimatedSize" 19000

  ; Windows 11: trust ZipNest's package certificate so the context-menu package
  ; (ZipNestShell.msix, which only registers ZipNestShell.dll) can be added.
  ${If} $WinBuild >= 22000
    DetailPrint "Preparing the Windows 11 context menu..."
    nsExec::ExecToLog 'certutil.exe -f -addstore TrustedPeople "$INSTDIR\ZipNest.cer"'
    Pop $0
  ${EndIf}

  ; The "ZipNest Setup" dialog: file types, shortcuts, shell integration.
  DetailPrint "Configuring ${APP}..."
  ${If} ${Silent}
    ExecWait '"$INSTDIR\ZipNest.exe" --setup' $0
    StrCpy $0 3
  ${Else}
    ExecWait '"$INSTDIR\ZipNest.exe" --setup-dialog' $0
  ${EndIf}

  SetShellVarContext all
  IntOp $1 $0 & 1
  ${If} $1 <> 0
    CreateShortcut "$DESKTOP\${APP}.lnk" "$INSTDIR\ZipNest.exe" "" "$INSTDIR\ZipNest.exe" 0
  ${Else}
    Delete "$DESKTOP\${APP}.lnk"
  ${EndIf}
  IntOp $1 $0 & 2
  ${If} $1 <> 0
    CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\ZipNest.exe" "" "$INSTDIR\ZipNest.exe" 0
  ${Else}
    Delete "$SMPROGRAMS\${APP}.lnk"
  ${EndIf}
SectionEnd

; ---------------------------------------------------------------- uninstall
Function un.onInit
  SetRegView 64
FunctionEnd

Section "Uninstall"
  DetailPrint "Removing file associations and the Explorer menu..."
  ExecWait '"$INSTDIR\ZipNest.exe" --remove-integration'
  ${If} "${CERT_SHA1}" != ""
    nsExec::ExecToLog 'certutil.exe -delstore TrustedPeople ${CERT_SHA1}'
    Pop $0
  ${EndIf}

  SetShellVarContext all
  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$DESKTOP\${APP}.lnk"

  Delete "$INSTDIR\ZipNest.exe"
  Delete "$INSTDIR\zipnest-cli.exe"
  Delete /REBOOTOK "$INSTDIR\ZipNestShell.dll"
  Delete /REBOOTOK "$INSTDIR\ZipNestShell.dll.old"
  Delete "$INSTDIR\ZipNestShell.msix"
  Delete "$INSTDIR\ZipNest.cer"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.md"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir /REBOOTOK "$INSTDIR"

  DeleteRegKey HKLM "${UNKEY}"
  DeleteRegKey HKLM "Software\${APP}"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\App Paths\ZipNest.exe"
  DeleteRegKey HKCU "Software\${APP}"
SectionEnd
