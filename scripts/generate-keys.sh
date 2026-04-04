#!/bin/bash
# ============================================
# 密钥生成脚本 - 校园资源采集系统
# 用于生成 JWT 认证所需的 RSA 密钥对
# ============================================

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}JWT 密钥生成工具${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# 检查是否有 keygen 可执行文件
if [ ! -f "$SCRIPT_DIR/keygen" ]; then
  echo -e "${RED}[错误] 未找到 keygen 可执行文件，请确保该文件与脚本在同一目录${NC}"
  exit 1
fi

# 进入脚本所在目录
cd "$SCRIPT_DIR"

echo "正在生成密钥对..."
./keygen -o keys -s 2048

if [ $? -ne 0 ]; then
    echo -e "${RED}[错误] 密钥生成失败！${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}密钥生成成功！${NC}"
echo -e "私钥：${YELLOW}keys/private.pem${NC}"
echo -e "公钥：${YELLOW}keys/public.pem${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo "重要提示:"
echo "1. 请妥善保管私钥文件，不要泄露"
echo "2. 不要将私钥提交到版本控制系统"
echo "3. 生产环境建议使用 4096 位密钥"
echo -e "${GREEN}============================================${NC}"
