; Discord Unlocker — instalador por usuário, sem privilégios administrativos.
; O binário precisa existir em ..\dist antes de chamar o ISCC.

#ifndef MyAppVersion
  #define MyAppVersion "0.1.5-dev"
#endif

#ifndef MyAppNumericVersion
  #define MyAppNumericVersion "0.1.5.0"
#endif

#define MyAppName "Discord Unlocker"
#define MyAppExeName "discord-unlocker.exe"

#ifdef SignedBuild
  #define MySetupBaseFilename "discord-unlocker-setup"
#else
  ; Validation builds are compiled with an explicit unsigned identity. The
  ; build script gives the completed artifact its public distribution name.
  #define MySetupBaseFilename "discord-unlocker-setup-unsigned"
#endif

[Setup]
AppId={{BCE0D7D4-2E4A-4A15-9D99-6E56A693287A}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=Discord Unlocker
MinVersion=10.0.10240
DefaultDirName={localappdata}\Programs\Discord Unlocker
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
OutputDir=..\dist
OutputBaseFilename={#MySetupBaseFilename}
Compression=none
SolidCompression=no
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayName={#MyAppName}
UninstallDisplayIcon={app}\discord.ico
ChangesAssociations=no
VersionInfoDescription=Discord Unlocker installer
VersionInfoCompany=Discord Unlocker project
VersionInfoCopyright=Copyright (c) 2026
VersionInfoOriginalFileName={#MySetupBaseFilename}.exe
VersionInfoProductName=Discord Unlocker
VersionInfoProductTextVersion={#MyAppVersion}
VersionInfoProductVersion={#MyAppNumericVersion}
VersionInfoTextVersion={#MyAppVersion}
VersionInfoVersion={#MyAppNumericVersion}
#ifdef SignedBuild
SignTool=release
SignedUninstaller=yes
#endif

[Files]
#ifdef SignedBuild
Source: "..\dist\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion signcheck
#else
Source: "..\dist\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
#endif
; Keep a stable local copy of Discord's own icon. This is copied from the
; installed client at setup time and is not embedded in the distribution.
Source: "{localappdata}\Discord\app.ico"; DestDir: "{app}"; DestName: "discord.ico"; Flags: external ignoreversion

[InstallDelete]
; Version 0.1.2 and earlier exposed separate generic-looking shortcuts. The
; managed Discord shortcuts created in [Code] replace them.
Type: files; Name: "{userprograms}\Discord Unlocker.lnk"
Type: files; Name: "{userdesktop}\Discord Unlocker.lnk"

[Run]
; A post-install checkbox lets the user close Discord only when they choose to.
; The next normal launch then receives the new per-process proxy arguments.
Filename: "{sys}\taskkill.exe"; Parameters: "/IM Discord.exe /T /F"; Description: "Encerrar o Discord agora para aplicar a nova inicialização"; Flags: postinstall skipifsilent runhidden

[UninstallDelete]
; Installer-state backups are deliberately excluded. [Code] removes them only
; after every shortcut and autostart value has been restored successfully.
Type: files; Name: "{localappdata}\Discord Unlocker\gateway-proxy.pac"
Type: files; Name: "{localappdata}\Discord Unlocker\proxy-cache-v1.json"
Type: files; Name: "{localappdata}\Discord Unlocker\discord-unlocker.log"
Type: files; Name: "{localappdata}\Discord Unlocker\discord-unlocker.log.1"
Type: files; Name: "{localappdata}\Discord Unlocker\discord-unlocker.log.2"
Type: files; Name: "{localappdata}\Discord Unlocker\discord-unlocker.log.3"
Type: dirifempty; Name: "{localappdata}\Discord Unlocker"

[Code]
const
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  StateKey = 'Software\Discord Unlocker';
  RunValue = 'Discord';
  BackupValue = 'OriginalDiscordRun';
  BackupExistsValue = 'OriginalDiscordRunExists';
  ShortcutStatePrefix = 'Shortcut.';
  ShortcutStateSuffix = '.OriginalExists';
  ManagedShortcutArguments = '--autostart';
  ReplaceFileWriteThrough = 1;

function ReplaceFile(
  ReplacedFileName, ReplacementFileName, BackupFileName: String;
  ReplaceFlags: Cardinal; Exclude, Reserved: Integer): Boolean;
  external 'ReplaceFileW@kernel32.dll stdcall';

function LauncherPath: String;
begin
  Result := ExpandConstant('{app}\{#MyAppExeName}');
end;

function DiscordIconPath: String;
begin
  Result := ExpandConstant('{app}\discord.ico');
end;

function StartRootShortcut: String;
begin
  Result := ExpandConstant('{userprograms}\Discord.lnk');
end;

function StartVendorShortcut: String;
begin
  Result := ExpandConstant('{userprograms}\Discord Inc\Discord.lnk');
end;

function DesktopShortcut: String;
begin
  Result := ExpandConstant('{userdesktop}\Discord.lnk');
end;

function TaskbarShortcut: String;
begin
  Result := ExpandConstant('{userappdata}\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\Discord.lnk');
end;

function ShortcutBackupDirectory: String;
begin
  Result := ExpandConstant('{localappdata}\Discord Unlocker\installer-state\shortcuts');
end;

function ShortcutBackupPath(const Id: String): String;
begin
  Result := ShortcutBackupDirectory + '\' + Id + '.lnk';
end;

function ShortcutStateValue(const Id: String): String;
begin
  Result := ShortcutStatePrefix + Id + ShortcutStateSuffix;
end;

function LauncherCommand: String;
begin
  Result := '"' + LauncherPath + '" --autostart';
end;

function IsLauncherCommand(const Value: String): Boolean;
begin
  Result := CompareText(Trim(Value), LauncherCommand) = 0;
end;

procedure BackupAndSetDiscordAutostart;
var
  Current: String;
  BackupKnown: String;
  Backup: String;
begin
  { Preserve the first observed value across reinstalls. }
  if not RegQueryStringValue(HKCU, StateKey, BackupExistsValue, BackupKnown) then begin
    if RegQueryStringValue(HKCU, RunKey, RunValue, Current) then begin
      if not RegWriteStringValue(HKCU, StateKey, BackupValue, Current) then
        RaiseException('Não foi possível salvar a inicialização original do Discord.');
      if not RegWriteStringValue(HKCU, StateKey, BackupExistsValue, '1') then
        RaiseException('Não foi possível confirmar o backup da inicialização do Discord.');
    end else begin
      if not RegWriteStringValue(HKCU, StateKey, BackupExistsValue, '0') then
        RaiseException('Não foi possível registrar a ausência da inicialização original do Discord.');
    end;
  end else if (BackupKnown <> '0') and (BackupKnown <> '1') then begin
    RaiseException('O estado do backup da inicialização do Discord está corrompido.');
  end else if (BackupKnown = '1') and
              (not RegQueryStringValue(HKCU, StateKey, BackupValue, Backup)) then begin
    RaiseException('O conteúdo do backup da inicialização do Discord está ausente.');
  end;

  { Never replace the original unless a complete backup state exists. }
  if not RegWriteStringValue(HKCU, RunKey, RunValue, LauncherCommand) then
    RaiseException('Não foi possível configurar a inicialização do Discord Unlocker.');
end;

function ReadShortcutState(const Id: String; var HadOriginal: Boolean): Boolean;
var
  Value: String;
begin
  Result := RegQueryStringValue(HKCU, StateKey, ShortcutStateValue(Id), Value);
  if not Result then
    exit;

  if Value = '1' then
    HadOriginal := True
  else if Value = '0' then
    HadOriginal := False
  else
    RaiseException('O estado salvo do atalho ' + Id + ' está corrompido.');
end;

procedure BackupShortcut(const Id, Path: String);
var
  HadOriginal: Boolean;
  BackupPath: String;
begin
  if ReadShortcutState(Id, HadOriginal) then begin
    if HadOriginal and (not FileExists(ShortcutBackupPath(Id))) then
      RaiseException('O backup original do atalho ' + Path + ' está ausente.');
    exit;
  end;

  if FileExists(Path) then begin
    if not ForceDirectories(ShortcutBackupDirectory) then
      RaiseException('Não foi possível criar a pasta de backup dos atalhos.');
    BackupPath := ShortcutBackupPath(Id);
    if not CopyFile(Path, BackupPath, False) then
      RaiseException('Não foi possível salvar o atalho original ' + Path + '.');
    if not RegWriteStringValue(HKCU, StateKey, ShortcutStateValue(Id), '1') then
      RaiseException('Não foi possível registrar o backup do atalho ' + Path + '.');
  end else begin
    if not RegWriteStringValue(HKCU, StateKey, ShortcutStateValue(Id), '0') then
      RaiseException('Não foi possível registrar a ausência do atalho ' + Path + '.');
  end;
end;

procedure BackupManagedShortcuts;
begin
  BackupShortcut('StartRoot', StartRootShortcut);
  BackupShortcut('StartVendor', StartVendorShortcut);
  BackupShortcut('Desktop', DesktopShortcut);
  BackupShortcut('Taskbar', TaskbarShortcut);
end;

function ShortcutPointsToLauncher(const Path: String): Boolean;
var
  Shell, Shortcut: Variant;
  Target, Arguments: String;
begin
  Result := False;
  if not FileExists(Path) then
    exit;

  try
    Shell := CreateOleObject('WScript.Shell');
    Shortcut := Shell.CreateShortcut(Path);
    Target := Shortcut.TargetPath;
    Arguments := Shortcut.Arguments;
    Target := Trim(Target);
    Arguments := Trim(Arguments);
    Result := (CompareText(Target, LauncherPath) = 0) and
      (CompareText(Arguments, ManagedShortcutArguments) = 0);
  except
    Log('Não foi possível inspecionar o atalho ' + Path + ': ' + GetExceptionMessage);
  end;
end;

procedure ReplaceShortcutAtomically(const Path, TemporaryPath: String);
begin
  if FileExists(Path) then begin
    if not ReplaceFile(Path, TemporaryPath, '', ReplaceFileWriteThrough, 0, 0) then
      RaiseException('Não foi possível substituir com segurança o atalho ' + Path +
        ': ' + SysErrorMessage(DLLGetLastError) + '.');
  end else begin
    if not RenameFile(TemporaryPath, Path) then
      RaiseException('Não foi possível instalar o atalho ' + Path + '.');
  end;
end;

procedure InstallManagedShortcut(
  const Id, Path: String; CreateWhenMissing: Boolean);
var
  HadOriginal: Boolean;
  TemporaryPath: String;
  Shell, Shortcut: Variant;
begin
  if not ReadShortcutState(Id, HadOriginal) then
    RaiseException('O estado original do atalho ' + Path + ' não foi salvo.');

  if (not FileExists(Path)) and (not CreateWhenMissing) then
    exit;

  if not ForceDirectories(ExtractFileDir(Path)) then
    RaiseException('Não foi possível criar a pasta do atalho ' + Path + '.');

  { WScript requires the temporary filename itself to end in .lnk. }
  TemporaryPath := Path + '.discord-unlocker.tmp.lnk';
  DeleteFile(TemporaryPath);
  if FileExists(Path) then begin
    if not CopyFile(Path, TemporaryPath, False) then
      RaiseException('Não foi possível preparar o atalho ' + Path + '.');
  end;

  try
    Shell := CreateOleObject('WScript.Shell');
    Shortcut := Shell.CreateShortcut(TemporaryPath);
    Shortcut.TargetPath := LauncherPath;
    Shortcut.Arguments := ManagedShortcutArguments;
    Shortcut.WorkingDirectory := ExpandConstant('{app}');
    Shortcut.IconLocation := DiscordIconPath + ',0';
    Shortcut.Description := 'Discord';
    Shortcut.Save;
  except
    DeleteFile(TemporaryPath);
    RaiseException('Não foi possível preparar o atalho Discord em ' + Path +
      ': ' + GetExceptionMessage + '.');
  end;

  ReplaceShortcutAtomically(Path, TemporaryPath);
  if not ShortcutPointsToLauncher(Path) then
    RaiseException('O atalho Discord instalado em ' + Path + ' não aponta para o launcher.');
end;

procedure InstallManagedShortcuts;
var
  HadVendor, HadTaskbar: Boolean;
begin
  if not FileExists(DiscordIconPath) then
    RaiseException('O ícone do Discord não foi copiado para o launcher.');

  InstallManagedShortcut('StartRoot', StartRootShortcut, True);

  if not ReadShortcutState('StartVendor', HadVendor) then
    RaiseException('O estado do atalho do Menu Iniciar está ausente.');
  InstallManagedShortcut('StartVendor', StartVendorShortcut, HadVendor);

  InstallManagedShortcut('Desktop', DesktopShortcut, True);

  if not ReadShortcutState('Taskbar', HadTaskbar) then
    RaiseException('O estado do atalho fixado está ausente.');
  InstallManagedShortcut('Taskbar', TaskbarShortcut, HadTaskbar);
end;

function RestoreManagedShortcut(const Id, Path: String): Boolean;
var
  HadOriginal: Boolean;
  BackupPath, TemporaryPath: String;
begin
  Result := False;
  if not ReadShortcutState(Id, HadOriginal) then begin
    Log('O estado original do atalho ' + Path + ' está ausente.');
    exit;
  end;

  if not FileExists(Path) then begin
    { A missing shortcut may have been removed intentionally after install. }
    Result := True;
    exit;
  end;

  if not ShortcutPointsToLauncher(Path) then begin
    { Preserve a user or Discord update made after this installation. }
    Log('Atalho alterado externamente; mantendo ' + Path + '.');
    Result := True;
    exit;
  end;

  if not HadOriginal then begin
    Result := DeleteFile(Path);
    if not Result then
      Log('Falha ao remover o atalho criado pelo launcher em ' + Path + '.');
    exit;
  end;

  BackupPath := ShortcutBackupPath(Id);
  if not FileExists(BackupPath) then begin
    Log('Backup do atalho ausente: ' + BackupPath + '.');
    exit;
  end;

  TemporaryPath := Path + '.discord-unlocker-restore.tmp.lnk';
  DeleteFile(TemporaryPath);
  if not CopyFile(BackupPath, TemporaryPath, False) then begin
    Log('Falha ao preparar a restauração de ' + Path + '.');
    exit;
  end;

  try
    ReplaceShortcutAtomically(Path, TemporaryPath);
    Result := True;
  except
    DeleteFile(TemporaryPath);
    Log('Falha ao restaurar ' + Path + ': ' + GetExceptionMessage + '.');
  end;
end;

function RestoreManagedShortcuts: Boolean;
begin
  Result := True;
  if not RestoreManagedShortcut('StartRoot', StartRootShortcut) then
    Result := False;
  if not RestoreManagedShortcut('StartVendor', StartVendorShortcut) then
    Result := False;
  if not RestoreManagedShortcut('Desktop', DesktopShortcut) then
    Result := False;
  if not RestoreManagedShortcut('Taskbar', TaskbarShortcut) then
    Result := False;
end;

function RestoreDiscordAutostart: Boolean;
var
  Current, Backup, HadOriginal: String;
  Restored: Boolean;
begin
  Restored := True;
  { Never overwrite a user or Discord change made after this installation. }
  if RegQueryStringValue(HKCU, RunKey, RunValue, Current) and
     IsLauncherCommand(Current) then begin
    if not RegQueryStringValue(HKCU, StateKey, BackupExistsValue, HadOriginal) then begin
      Restored := False;
    end else if HadOriginal = '0' then begin
      Restored := RegDeleteValue(HKCU, RunKey, RunValue);
    end else if (HadOriginal = '1') and
                RegQueryStringValue(HKCU, StateKey, BackupValue, Backup) then begin
      Restored := RegWriteStringValue(HKCU, RunKey, RunValue, Backup);
    end else begin
      Restored := False;
    end;
  end;

  if not Restored then begin
    Log('Falha ao restaurar a inicialização do Discord; backup mantido em HKCU\' + StateKey + '.');
  end;
  Result := Restored;
end;

procedure CleanupSavedState;
begin
  if DirExists(ExpandConstant('{localappdata}\Discord Unlocker\installer-state')) and
     (not DelTree(ExpandConstant('{localappdata}\Discord Unlocker\installer-state'), True, True, True)) then
    Log('Falha ao remover os backups de atalhos já restaurados.');
  if RegKeyExists(HKCU, StateKey) and
     (not RegDeleteKeyIncludingSubkeys(HKCU, StateKey)) then
    Log('Falha ao remover o estado já restaurado em HKCU\' + StateKey + '.');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
    BackupManagedShortcuts
  else if CurStep = ssPostInstall then begin
    InstallManagedShortcuts;
    BackupAndSetDiscordAutostart;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then begin
    if not RestoreDiscordAutostart then
      RaiseException('A desinstalação foi interrompida porque a inicialização original do Discord não pôde ser restaurada com segurança. Tente novamente.');
    if not RestoreManagedShortcuts then
      RaiseException('A desinstalação foi interrompida porque os atalhos originais do Discord não puderam ser restaurados com segurança. Tente novamente.');
    CleanupSavedState;
  end;
end;
