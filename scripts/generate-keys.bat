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

REM 检查 Go 环境
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo [错误] 未检测到 Go 环境，请先安装 Go: https://go.dev/dl/
    pause
    exit /b 1
)

REM 进入项目目录
cd /d "%~dp0.."

echo 正在生成密钥对...
go run cmd/keygen/main.go -o keys -s 2048

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
