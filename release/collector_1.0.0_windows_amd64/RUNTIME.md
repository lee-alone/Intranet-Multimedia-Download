# 校园资源采集系统 - 运行说明

## 首次运行步骤

1. 生成 JWT 密钥对（仅首次运行需要）:
- Windows: 运行 generate-keys.bat
- Linux: 运行 ./generate-keys.sh
- 或手动执行：go run cmd/keygen/main.go

2. 配置应用程序:
- 复制 config.yaml.example 为 config.yaml
- 修改配置文件中的数据库路径、端口等设置

3. 运行程序:
- Windows: .\collector.exe
- Linux: ./collector

## 目录结构
- bin/ - 编译后的可执行文件
- config.yaml - 配置文件
- data/ - 数据库文件目录
- keys/ - JWT 密钥文件目录
- logs/ - 日志文件目录
- runtime/ - 外部下载工具 (yt-dlp, lux)
- downloads/ - 下载完成文件目录

## 注意事项
1. 私钥文件 (keys/private.pem) 请妥善保管，不要泄露
2. 不要将私钥提交到版本控制系统
3. 生产环境建议使用 4096 位密钥
