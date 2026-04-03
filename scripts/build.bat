@echo off
chcp 936 >nul
REM ============================================
REM Windows 编译脚本 - 校园资源采集系统
REM 需要安装 TDM-GCC 或 MinGW 来支持 CGO
REM 需要安装 Node.js 来构建前端资源
REM ============================================

setlocal enabledelayedexpansion

REM 设置变量
set PROJECT_NAME=collector
set OUTPUT_DIR=bin
set RELEASE_DIR=release
set MAIN_PATH=cmd/server/main.go
set BUILD_TIME=%DATE% %TIME%
set VERSION=1.0.0

REM 获取脚本所在目录的绝对路径 (scripts 目录)
set SCRIPT_DIR=%~dp0
REM 计算项目根目录 (去掉末尾的 'scripts\' 部分，保留末尾反斜杠)
set PROJECT_ROOT=%SCRIPT_DIR:~0,-9%

REM 检查 Go 环境
echo ^[1/6^] 检查 Go 环境...
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[错误^] 未检测到 Go 环境，请先安装 Go: https://go.dev/dl/
    pause
    exit /b 1
)
go version

REM 检查 GCC 环境 (CGO 需要)
echo ^[2/6^] 检查 GCC 环境...
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[错误^] 未检测到 GCC 环境，请安装 TDM-GCC 或 MinGW
    echo 下载地址：https://jmeubank.github.io/tdm-gcc/
    pause
    exit /b 1
)
gcc --version | findstr /C:"gcc"
echo GCC 环境检查通过

REM 检查 Node.js 环境
echo ^[3/6^] 检查 Node.js 环境...
where node >nul 2>&1
if %errorlevel% neq 0 (
    echo ^[警告^] 未检测到 Node.js 环境
    echo 如果前端资源已构建，可跳过此步骤
    echo 下载地址：https://nodejs.org/
) else (
    node --version
    echo Node.js 环境检查通过
)

REM 创建输出目录
echo ^[4/6^] 创建输出目录...
if not exist "%PROJECT_ROOT%\%OUTPUT_DIR%" (
  mkdir "%PROJECT_ROOT%\%OUTPUT_DIR%"
  echo 已创建目录：%PROJECT_ROOT%\%OUTPUT_DIR%
)
if not exist "%PROJECT_ROOT%\%RELEASE_DIR%" (
  mkdir "%PROJECT_ROOT%\%RELEASE_DIR%"
  echo 已创建目录：%PROJECT_ROOT%\%RELEASE_DIR%
)

REM 构建前端资源
echo ^[5/6^] 构建前端资源...
pushd "%PROJECT_ROOT%\web"
if exist "node_modules" (
  echo 检测到 node_modules，执行 npm build...
  call npm run build
  if %errorlevel% neq 0 (
    echo ^[错误^] 前端构建失败！
    popd
    pause
    exit /b 1
  )
) else (
  echo 未检测到 node_modules，先执行 npm install...
  call npm install
  if %errorlevel% neq 0 (
    echo ^[错误^] npm install 失败！
    popd
    pause
    exit /b 1
  )
  call npm run build
  if %errorlevel% neq 0 (
    echo ^[错误^] 前端构建失败！
    popd
    pause
    exit /b 1
  )
)
popd
echo 前端构建完成

REM 检查 web/dist 是否存在
if not exist "%PROJECT_ROOT%\web\dist" (
  echo ^[错误^] 前端构建产物目录 web\dist 不存在！
  echo 请检查前端构建是否成功
  pause
  exit /b 1
)

REM 编译项目
echo ^[6/6^] 开始编译 Windows 版本 (CGO 启用)...
REM 设置环境变量
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
set CC=gcc

REM 显示编译信息
echo Go Version:
go version
echo GCC Path:
where gcc
echo 开始编译...

go build -ldflags="-s -w -X 'main.buildTime=%BUILD_TIME%'" -o "%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe" "%PROJECT_ROOT%\%MAIN_PATH%"

if %errorlevel% neq 0 (
    echo ^[错误^] 编译失败！
    echo 请确保已正确安装 TDM-GCC 或 MinGW
    pause
    exit /b 1
)

echo.
echo ============================================
echo [成功] 编译完成！
echo 输出文件：%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe
echo ============================================

REM 打包发布文件
echo.
echo 开始打包发布文件...
set RELEASE_NAME=%PROJECT_NAME%_%VERSION%_windows_amd64
set RELEASE_PATH=%PROJECT_ROOT%\%RELEASE_DIR%\%RELEASE_NAME%

if exist "%RELEASE_PATH%" (
    rmdir /s /q "%RELEASE_PATH%"
)
mkdir "%RELEASE_PATH%"

REM 复制必要文件
copy "%PROJECT_ROOT%\%OUTPUT_DIR%\%PROJECT_NAME%.exe" "%RELEASE_PATH%\"
copy "%PROJECT_ROOT%\config.yaml.example" "%RELEASE_PATH%\config.yaml"
copy "%PROJECT_ROOT%\README.md" "%RELEASE_PATH%\"

REM 注意：migrations 目录已嵌入到二进制文件中，不需要复制

REM 复制 runtime 目录（包含 yt-dlp 和 lux）
if exist "%PROJECT_ROOT%\runtime" (
  xcopy /E /I /Y "%PROJECT_ROOT%\runtime" "%RELEASE_PATH%\runtime"
  echo 已复制 runtime 目录
)

REM 创建目录结构
mkdir "%RELEASE_PATH%\data"
mkdir "%RELEASE_PATH%\logs"
mkdir "%RELEASE_PATH%\keys"

REM 复制密钥文件（如果存在）
if exist "%PROJECT_ROOT%\keys\private.pem" (
  copy "%PROJECT_ROOT%\keys\private.pem" "%RELEASE_PATH%\keys\"
  echo 已复制私钥文件
)
if exist "%PROJECT_ROOT%\keys\public.pem" (
  copy "%PROJECT_ROOT%\keys\public.pem" "%RELEASE_PATH%\keys\"
  echo 已复制公钥文件
)

REM 复制密钥生成脚本
copy "%PROJECT_ROOT%\scripts\generate-keys.bat" "%RELEASE_PATH%\"

REM 创建.gitignore 文件
(
echo # 忽略运行时生成的文件
echo *.db
echo *.sqlite
echo *.sqlite3
echo.
echo # 忽略日志文件
echo logs/*
echo ^!logs/.gitkeep
echo.
echo # 忽略数据文件
echo data/*
echo ^!data/.gitkeep
echo.
echo # 忽略下载文件
echo downloads/*
echo ^!downloads/.gitkeep
echo.
echo # 忽略密钥文件（重要！）
echo keys/private.pem
echo keys/*.pem
echo ^!keys/.gitkeep
echo ^!keys/public.pem
echo.
echo # 忽略配置文件
echo config.yaml
) > "%RELEASE_PATH%\.gitignore"

REM 创建 README 说明
(
echo # 校园资源采集系统 - 运行说明
echo.
echo ## 首次运行步骤
echo.
echo 1. 生成 JWT 密钥对（仅首次运行需要）:
echo - Windows: 运行 generate-keys.bat
echo - Linux: 运行 ./generate-keys.sh
echo - 或手动执行：go run cmd/keygen/main.go
echo.
echo 2. 配置应用程序:
echo - 复制 config.yaml.example 为 config.yaml
echo - 修改配置文件中的数据库路径、端口等设置
echo.
echo 3. 运行程序:
echo - Windows: .\collector.exe
echo - Linux: ./collector
echo.
echo ## 目录结构
echo - bin/ - 编译后的可执行文件
echo - config.yaml - 配置文件
echo - data/ - 数据库文件目录
echo - keys/ - JWT 密钥文件目录
echo - logs/ - 日志文件目录
echo - runtime/ - 外部下载工具 ^(yt-dlp, lux^)
echo - downloads/ - 下载完成文件目录
echo.
echo ## 注意事项
echo 1. 私钥文件 ^(keys/private.pem^) 请妥善保管，不要泄露
echo 2. 不要将私钥提交到版本控制系统
echo 3. 生产环境建议使用 4096 位密钥
) > "%RELEASE_PATH%\RUNTIME.md"

echo.
echo ============================================
echo [成功] 发布包已创建！
echo 发布路径：%RELEASE_PATH%
echo ============================================
pause
