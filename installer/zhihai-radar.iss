; 知海选品雷达 · Inno Setup 6 安装包
; 目标：评委双击安装、免管理员、卸载时程序与数据分离。
;
; 构建前置：仓库根目录先执行
;   go build -ldflags "-X main.version=0.1.0" -o installer/stage/zhihai-radar.exe ./cmd/desktop
;   把 rferp-server.exe 放到 installer/stage/rferp-server.exe（运行环境，只发二进制）
;   把离线快照复制到 installer/stage/fixtures/（三个品类）
; 然后：ISCC installer/zhihai-radar.iss

#define MyAppName "知海选品雷达"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "rf-us"
#define MyAppExeName "zhihai-radar.exe"
#define MyAppDataDir "{localappdata}\zhihai-radar"
#define MyProgramDir "{localappdata}\Programs\zhihai-radar"

[Setup]
AppId={{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={#MyProgramDir}
DefaultGroupName={#MyAppName}
; 免管理员：写 LocalAppData，不弹 UAC
PrivilegesRequired=lowest
OutputDir=Output
OutputBaseFilename=Setup-ZhihaiRadar-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; 卸载程序也放程序目录，避免写 System32
Uninstallable=yes
CreateAppDir=yes

[Languages]
Name: "chinese"; MessagesFile: ".\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式:"; Flags: unchecked
Name: "removedata"; Description: "卸载时同时删除演示数据与缓存"; GroupDescription: "卸载选项:"; Flags: unchecked

[Files]
; 白名单：只放必要文件，不打包源码 / .pdb / go.mod
Source: "stage\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "stage\rferp-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "stage\fixtures\*"; DestDir: "{app}\fixtures"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\THIRD-PARTY-NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
; 字体许可（Noto Sans SC 为 OFL）：随源码发布，安装包只带声明文件
Source: "..\docs\FONT-LICENSE.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即运行 {#MyAppName}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; 程序目录由 Inno 自动清空；用户数据默认保留，只有勾选才删。
Type: filesandordirs; Name: "{#MyAppDataDir}"; Tasks: removedata

[Code]
// 覆盖安装不丢数据：安装时不碰 {#MyAppDataDir}。
// 首次启动引导（数据源模式/端口确认）由客户端内的三步向导实现，安装包只负责放文件。
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    // 强制创建数据目录，保证首次启动不用再请求权限。
    if not DirExists(ExpandConstant('{#MyAppDataDir}')) then
      CreateDir(ExpandConstant('{#MyAppDataDir}'));
  end;
end;
