#!/bin/bash
# ============================================
# Linux 编译脚本 - 校园资源采集系统
# 需要安装 gcc 来支持 CGO
# 需要安装 Node.js 来构建前端资源
# ============================================

set -e

# 设置变量
PROJECT_NAME="collector"
OUTPUT_DIR="bin"
RELEASE_DIR="release"
MAIN_PATH="cmd/server/main.go"
BUILD_TIME=$(date '+%Y-%m-%d %H:%M:%S')
VERSION="1.0.0"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}[1/6] 检查 Go 环境...${NC}"
if ! command -v go &> /dev/null; then
    echo -e "${RED}[错误] 未检测到 Go 环境，请先安装 Go${NC}"
    echo "Ubuntu/Debian: sudo apt-get install golang-go"
    echo "CentOS/RHEL: sudo yum install golang"
    exit 1
fi
go version

echo -e "${GREEN}[2/6] 检查 GCC 环境...${NC}"
if ! command -v gcc &> /dev/null; then
    echo -e "${RED}[错误] 未检测到 GCC 环境${NC}"
    echo "Ubuntu/Debian: sudo apt-get install build-essential"
    echo "CentOS/RHEL: sudo yum install gcc"
    exit 1
fi
gcc --version | head -n1
echo -e "${GREEN}GCC 环境检查通过${NC}"

echo -e "${GREEN}[3/6] 检查 Node.js 环境...${NC}"
if ! command -v node &> /dev/null; then
    echo -e "${YELLOW}[警告] 未检测到 Node.js 环境${NC}"
    echo "如果前端资源已构建，可跳过此步骤"
    echo "Ubuntu/Debian: sudo apt-get install nodejs npm"
    echo "CentOS/RHEL: sudo yum install nodejs npm"
else
    node --version
    echo -e "${GREEN}Node.js 环境检查通过${NC}"
fi

echo -e "${GREEN}[4/6] 创建输出目录...${NC}"
mkdir -p "$OUTPUT_DIR"
mkdir -p "$RELEASE_DIR"

echo -e "${GREEN}[5/6] 构建前端资源...${NC}"
cd web
if [ -d "node_modules" ]; then
    echo "检测到 node_modules，执行 npm build..."
    npm run build
else
    echo "未检测到 node_modules，先执行 npm install..."
    npm install
    npm run build
fi
cd ..
echo -e "${GREEN}前端构建完成${NC}"

echo -e "${GREEN}[6/6] 开始编译 Linux 版本 (CGO 启用)...${NC}"
export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64
export CC=gcc

echo "Go Version: $(go version)"
echo "GCC Version: $(gcc --version | head -n1)"
echo "开始编译..."

go build -ldflags="-s -w -X 'main.buildTime=$BUILD_TIME'" -o "$OUTPUT_DIR/$PROJECT_NAME" "$MAIN_PATH"

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}[成功] 编译完成！${NC}"
echo -e "输出文件：${YELLOW}$OUTPUT_DIR/$PROJECT_NAME${NC}"
echo -e "${GREEN}============================================${NC}"

# 打包发布文件
echo ""
echo "开始打包发布文件..."
RELEASE_NAME="${PROJECT_NAME}_${VERSION}_linux_amd64"
RELEASE_PATH="$RELEASE_DIR/$RELEASE_NAME"

rm -rf "$RELEASE_PATH"
mkdir -p "$RELEASE_PATH"

# 复制必要文件
cp "$OUTPUT_DIR/$PROJECT_NAME" "$RELEASE_PATH/"
cp "config.yaml.example" "$RELEASE_PATH/config.yaml"
cp "README.md" "$RELEASE_PATH/"
cp -r "migrations" "$RELEASE_PATH/"

# 复制密钥文件（如果存在）
if [ -f "keys/private.pem" ]; then
    cp "keys/private.pem" "$RELEASE_PATH/keys/"
fi
if [ -f "keys/public.pem" ]; then
    cp "keys/public.pem" "$RELEASE_PATH/keys/"
fi

# 复制密钥生成脚本
cp "scripts/generate-keys.sh" "$RELEASE_PATH/"

# 创建目录结构
mkdir -p "$RELEASE_PATH/data"
mkdir -p "$RELEASE_PATH/logs"
mkdir -p "$RELEASE_PATH/keys"

# 创建.gitignore 文件
cat > "$RELEASE_PATH/.gitignore" << 'EOF'
# 忽略运行时生成的文件
*.db
*.sqlite
*.sqlite3

# 忽略日志文件
logs/*
!logs/.gitkeep

# 忽略数据文件
data/*
!data/.gitkeep

# 忽略临时文件
temp/*
!temp/.gitkeep
downloads/*
!downloads/.gitkeep

# 忽略密钥文件（重要！）
keys/private.pem
keys/*.pem
!keys/.gitkeep
!keys/public.pem

# 忽略配置文件
config.yaml
EOF

# 创建 README 说明
cat > "$RELEASE_PATH/RUNTIME.md" << 'EOF'
# 校园资源采集系统 - 运行说明

## 首次运行步骤

1. 生成 JWT 密钥对（仅首次运行需要）:
   - Linux: 运行 ./generate-keys.sh
   - 或手动执行：go run cmd/keygen/main.go

2. 配置应用程序:
   - 复制 config.yaml.example 为 config.yaml
   - 修改配置文件中的数据库路径、端口等设置

3. 运行程序:
   - Linux: ./collector
   - Windows: .\collector.exe

## 目录结构
- bin/ - 编译后的可执行文件
- config.yaml - 配置文件
- data/ - 数据库文件目录
- keys/ - JWT 密钥文件目录
- logs/ - 日志文件目录
- migrations/ - 数据库迁移文件
- temp/ - 临时下载目录
- downloads/ - 下载完成文件目录

## 注意事项
1. 私钥文件 (keys/private.pem) 请妥善保管，不要泄露
2. 不要将私钥提交到版本控制系统
3. 生产环境建议使用 4096 位密钥
EOF

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}[成功] 发布包已创建！${NC}"
echo -e "发布路径：${YELLOW}$RELEASE_PATH${NC}"
echo -e "${GREEN}============================================${NC}"
