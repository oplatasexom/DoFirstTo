; Установщик DoFirstTo. Ставит программу в профиль пользователя, права
; администратора не нужны
;
;   makensis /DVERSION=1.2.0 /DARCH=amd64 build\installer\dofirst.nsi
;
; Данные пользователя (%APPDATA%\DoFirst) при удалении не трогаются

Unicode true
RequestExecutionLevel user

!ifndef VERSION
  !define VERSION "0.1.0"
!endif
!ifndef ARCH
  !define ARCH "amd64"
!endif

!define APP "DoFirstTo"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP}"
!define RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${APP} ${VERSION}"
OutFile "..\..\dist\${APP}-${VERSION}-windows-${ARCH}-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\${APP}"
InstallDirRegKey HKCU "${UNINSTALL_KEY}" "InstallLocation"
SetCompressor /SOLID lzma

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "Install"
  SetOutPath "$INSTDIR"
  File "/oname=${APP}.exe" "..\..\dist\dofirst-windows-${ARCH}.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\${APP}.exe"

  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\${APP}.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  ; Автозапуск программа включает сама — убираем его вместе с ней.
  DeleteRegValue HKCU "${RUN_KEY}" "${APP}"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"

  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$INSTDIR\${APP}.exe"
  Delete "$INSTDIR\${APP}.exe.old"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
SectionEnd
