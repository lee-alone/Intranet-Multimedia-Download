@echo off
chcp 936 >nul
REM ============================================
REM Windows ����ű� - У԰��Դ�ɼ�ϵͳ
REM ��Ҫ��װ TDM-GCC �� MinGW ��֧�� CGO
REM ��Ҫ��װ Node.js ������ǰ����Դ
REM ============================================

setlocal enabledelayedexpansion

REM ���ñ���
set PROJECT_NAME=collector
set OUTPUT_DIR=bin
set RELEASE_DIR=release
set MAIN_PATH=cmd/server/main.go
set BUILD_TIME=%DATE% %TIME%
set VERSION=1.0.0

REM ��ȡ�ű�����Ŀ¼�ľ���·�� (scripts Ŀ¼)
set SCRIPT_DIR=%~dp0
REM ������Ŀ��Ŀ¼ (ȥ��ĩβ�� 'scripts\' ���֣�����ĩβ��б��)
set PROJECT_ROOT=%SCRIPT_DIR:~0,-9%

REM ��� Go ����
echo ^[1/6^] ��� Go ����...
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[����^] δ��⵽ Go ���������Ȱ�װ Go: https://go.dev/dl/
    pause
    exit /b 1
)
go version

REM ��� GCC ���� (CGO ��Ҫ)
echo ^[2/6^] ��� GCC ����...
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[����^] δ��⵽ GCC �������밲װ TDM-GCC �� MinGW
    echo ���ص�ַ��https://jmeubank.github.io/tdm-gcc/
    pause
    exit /b 1
)
gcc --version | findstr /C:"gcc"
echo GCC �������ͨ��

REM ��� Node.js ����
echo ^[3/6^] ��� Node.js ����...
where node >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[����^] δ��⵽ Node.js ����
    echo ���ǰ����Դ�ѹ������������˲���
    echo ���ص�ַ��https://nodejs.org/
) else (
    node --version
    echo Node.js �������ͨ��
)

REM �������Ŀ¼
echo ^[4/6^] �������Ŀ¼...
if not exist "%PROJECT_ROOT%\%OUTPUT_DIR%" (
  mkdir "%PROJECT_ROOT%\%OUTPUT_DIR%"
  echo �Ѵ���Ŀ¼��%PROJECT_ROOT%\%OUTPUT_DIR%
)
if not exist "%PROJECT_ROOT%\%RELEASE_DIR%" (
  mkdir "%PROJECT_ROOT%\%RELEASE_DIR%"
  echo �Ѵ���Ŀ¼��%PROJECT_ROOT%\%RELEASE_DIR%
)

REM ����ǰ����Դ
echo ^[5/6^] ����ǰ����Դ...
pushd "%PROJECT_ROOT%\web"
if exist "node_modules" (
  echo ��⵽ node_modules��ִ�� npm build...
  call npm run build
  if %errorlevel% neq 0 (
    echo ^[����^] ǰ�˹���ʧ�ܣ�
    popd
    pause
    exit /b 1
  )
) else (
  echo δ��⵽ node_modules����ִ�� npm install...
  call npm install
  if %errorlevel% neq 0 (
    echo ^[����^] npm install ʧ�ܣ�
    popd
    pause
    exit /b 1
  )
  call npm run build
  if %errorlevel% neq 0 (
    echo ^[����^] ǰ�˹���ʧ�ܣ�
    popd
    pause
    exit /b 1
  )
)
popd
echo ǰ�˹������

REM ��� web/dist �Ƿ����
if not exist "%PROJECT_ROOT%\web\dist" (
  echo ^[����^] ǰ�˹�������Ŀ¼ web\dist �����ڣ�
  echo ����ǰ�˹����Ƿ�ɹ�
  pause
  exit /b 1
)

REM ������Ŀ
echo ^[6/6^] ��ʼ���� Windows �汾 (CGO ����)...
REM ���û�������
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
set CC=gcc

REM ��ʾ������Ϣ
echo Go Version:
go version
echo GCC Path:
where gcc
echo ��ʼ����...

go build -ldflags="-s -w -X 'main.buildTime=%BUILD_TIME%'" -o "%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe" "%PROJECT_ROOT%\%MAIN_PATH%"

if %errorlevel% neq 0 (
  echo ^[错误] 编译失败！
  echo 请确保已正确安装 TDM-GCC 或 MinGW
  pause
  exit /b 1
)

REM 编译密钥生成工具
echo ^[编译密钥生成工具^]...
go build -o "%PROJECT_ROOT%\%OUTPUT_DIR%\keygen.exe" "%PROJECT_ROOT%\cmd\keygen\main.go"

if %errorlevel% neq 0 (
    echo ^[����^] ����ʧ�ܣ�
    echo ��ȷ������ȷ��װ TDM-GCC �� MinGW
    pause
    exit /b 1
)

echo.
echo ============================================
echo [�ɹ�] ������ɣ�
echo ����ļ���%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe
echo ============================================

REM ��������ļ�
echo.
echo ��ʼ��������ļ�...
set RELEASE_NAME=%PROJECT_NAME%_%VERSION%_windows_amd64
set RELEASE_PATH=%PROJECT_ROOT%\%RELEASE_DIR%\%RELEASE_NAME%

if exist "%RELEASE_PATH%" (
    rmdir /s /q "%RELEASE_PATH%"
)
mkdir "%RELEASE_PATH%"

REM ���Ʊ�Ҫ�ļ�
copy "%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe" "%RELEASE_PATH%\"
copy "%PROJECT_ROOT%\config.yaml.example" "%RELEASE_PATH%\config.yaml"
copy "%PROJECT_ROOT%\README.md" "%RELEASE_PATH%\"

REM ע�⣺migrations Ŀ¼��Ƕ�뵽�������ļ��У�����Ҫ����

REM ���� runtime Ŀ¼������ yt-dlp �� lux��
REM 检查 runtime 目录是否存在以及关键文件
if exist "%PROJECT_ROOT%\runtime" (
  echo [检查 runtime 目录]...
  
  REM 检查 runtime 目录是否为空
  set "HAS_FILES=0"
  for %%f in ("%PROJECT_ROOT%\runtime\*") do set "HAS_FILES=1"
  if "%HAS_FILES%"=="0" (
    echo [警告] runtime 目录为空，将跳过复制
  ) else (
    xcopy /E /I /Y "%PROJECT_ROOT%\runtime" "%RELEASE_PATH%\runtime"
    echo [已复制] runtime 目录
  )
) else (
  echo [警告] runtime 目录不存在，将跳过复制外部工具 (yt-dlp, lux)
  echo 如需使用视频下载功能，请创建 runtime 目录并放入 yt-dlp 和 lux 可执行文件
)

REM ����Ŀ¼�ṹ
mkdir "%RELEASE_PATH%\data"
mkdir "%RELEASE_PATH%\logs"
mkdir "%RELEASE_PATH%\keys"

REM ������Կ�ļ���������ڣ�
if exist "%PROJECT_ROOT%\keys\private.pem" (
  copy "%PROJECT_ROOT%\keys\private.pem" "%RELEASE_PATH%\keys\"
  echo �Ѹ���˽Կ�ļ�
)
if exist "%PROJECT_ROOT%\keys\public.pem" (
  copy "%PROJECT_ROOT%\keys\public.pem" "%RELEASE_PATH%\keys\"
  echo �Ѹ��ƹ�Կ�ļ�
)

REM ������Կ���ɽű�
copy "%PROJECT_ROOT%\scripts\generate-keys.bat" "%RELEASE_PATH%\"

REM �����ܹ�Կ��������� (���Ƶ� release Ŀ¼)
copy "%PROJECT_ROOT%\%OUTPUT_DIR%\keygen.exe" "%RELEASE_PATH%\"
echo 已复制 keygen.exe 到 release 目录

REM ����.gitignore �ļ�
(
echo # ��������ʱ���ɵ��ļ�
echo *.db
echo *.sqlite
echo *.sqlite3
echo.
echo # ������־�ļ�
echo logs/*
echo ^!logs/.gitkeep
echo.
echo # ���������ļ�
echo data/*
echo ^!data/.gitkeep
echo.
echo # ���������ļ�
echo downloads/*
echo ^!downloads/.gitkeep
echo.
echo # ������Կ�ļ�����Ҫ����
echo keys/private.pem
echo keys/*.pem
echo ^!keys/.gitkeep
echo ^!keys/public.pem
echo.
echo # ���������ļ�
echo config.yaml
) > "%RELEASE_PATH%\.gitignore"

REM ���� README ˵��
(
echo # У԰��Դ�ɼ�ϵͳ - ����˵��
echo.
echo ## �״����в���
echo.
echo 1. ���� JWT ��Կ�ԣ����״�������Ҫ��:
echo - Windows: ֱ�Ӱ� double-click generate-keys.bat
echo - Linux: ���� ./generate-keys.sh
echo - ���ֶ�ִ�У�.\\keygen.exe -o keys -s 2048
echo.
echo 2. ����Ӧ�ó���:
echo - ���� config.yaml.example Ϊ config.yaml
echo - �޸������ļ��е����ݿ�·�����˿ڵ�����
echo.
echo 3. ���г���:
echo - Windows: .\collector.exe
echo - Linux: ./collector
echo.
echo ## Ŀ¼�ṹ
echo - bin/ - �����Ŀ�ִ���ļ�
echo - config.yaml - �����ļ�
echo - data/ - ���ݿ��ļ�Ŀ¼
echo - keys/ - JWT ��Կ�ļ�Ŀ¼
echo - logs/ - ��־�ļ�Ŀ¼
echo - runtime/ - �ⲿ���ع��� ^(yt-dlp, lux^)
echo - downloads/ - ��������ļ�Ŀ¼
echo.
echo ## ע������
echo 1. ˽Կ�ļ� ^(keys/private.pem^) �����Ʊ��ܣ���Ҫй¶
echo 2. ��Ҫ��˽Կ�ύ���汾����ϵͳ
echo 3. ������������ʹ�� 4096 λ��Կ
) > "%RELEASE_PATH%\RUNTIME.md"

echo.
echo ============================================
echo [�ɹ�] �������Ѵ�����
echo ����·����%RELEASE_PATH%
echo ============================================
pause
