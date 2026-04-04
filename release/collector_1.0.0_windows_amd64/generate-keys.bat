@echo off
REM ============================================
REM 密钥生成脚本 - 校园资源采集系统
REM 用于生成 JWT 认证所需的 RSA 密钥对
REM ============================================

setlocal

echo ============================================
echo JWT 密钥生成工具
echo ============================================
echo.

REM 获取当前脚本所在目录（即程序所在目录）
set SCRIPT_DIR=%~dp0

REM 检查是否有 keygen.exe
if not exist "%SCRIPT_DIR%keygen.exe" (
  echo [错误] 未找到 keygen.exe，请确保该文件与脚本在同一目录
  pause
  exit /b 1
)

REM 进入脚本所在目录
cd /d "%SCRIPT_DIR%"

echo 正在生成密钥对...
keygen.exe -o keys -s 2048

if %errorlevel% neq 0 (
    echo [错误] 密钥生成失败！
    pause
    exit /b 1
)

echo.
echo ============================================
echo 密钥生成成功！
echo 私钥：keys/private.pem
echo 公钥：keys/public.pem
echo ============================================
echo.
echo 重要提示:
echo 1. 请妥善保管私钥文件，不要泄露
echo 2. 不要将私钥提交到版本控制系统
echo 3. 生产环境建议使用 4096 位密钥
echo ============================================
pause
