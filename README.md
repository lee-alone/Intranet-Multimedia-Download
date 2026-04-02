# 校园资源采集系统

> 版本: v4.0  
> 更新日期: 2026-03-25

校园资源采集系统是一个支持多平台视频资源下载的 Web 应用，提供双引擎下载、批量任务管理、实时进度推送等功能。

## 功能特性

- 🔐 **安全认证**: 支持 JWT 认证和 LDAP 集成，支持 MFA 二次验证
- 📥 **双引擎下载**: yt-dlp + Lux 双引擎，自动故障转移
- 📦 **批量任务**: 支持批量 URL 提交，任务优先级调整
- 📊 **实时进度**: WebSocket 实时推送下载进度
- 🔒 **安全审计**: 完整的操作审计日志，敏感信息脱敏
- 🎯 **域名无限制**: 支持下载任意网站的资源，无域名限制

## 技术栈

### 后端
- Go 1.20+
- SQLite (WAL 模式)
- Gin/Echo Web 框架
- JWT 认证
- WebSocket

### 前端
- Vue 3
- Vite
- Tailwind CSS
- TypeScript

### 外部依赖
- yt-dlp (视频下载)
- FFmpeg (视频合并/转码)
- Lux (备用下载引擎)

## 快速开始

### 环境要求

| 工具 | 版本 | 说明 |
|------|------|------|
| Go | 1.20+ | 后端开发 |
| Node.js | 18+ | 前端开发 |
| SQLite | 3.x | 数据库 |
| yt-dlp | latest | 视频下载 |
| FFmpeg | 4.0+ | 视频处理 |

### 安装步骤

1. **克隆仓库**
```bash
git clone https://github.com/campus/collector.git
cd collector
```

2. **安装后端依赖**
```bash
go mod download
```

3. **安装前端依赖**
```bash
cd web
npm install
cd ..
```

4. **配置文件**
```bash
# 复制示例配置
cp config.yaml.example config.yaml

# 编辑配置文件
vim config.yaml
```

5. **启动服务**
```bash
# 启动后端
go run cmd/server/main.go

# 启动前端开发服务器
cd web && npm run dev
```

### 访问服务

- 后端 API: http://localhost:8080
- 健康检查: http://localhost:8080/health
- 前端界面: http://localhost:5173

## 项目结构

```
collector/
├── cmd/                    # 应用入口
│   ├── server/            # Web 服务器
│   └── keygen/            # JWT 密钥生成工具
├── internal/              # 内部包
│   ├── auth/              # 认证模块
│   ├── audit/             # 审计日志
│   ├── config/            # 配置管理
│   ├── database/          # 数据库操作
│   ├── download/          # 下载引擎
│   ├── engine/            # 引擎适配层
│   ├── scheduler/         # 任务调度
│   └── server/            # HTTP 服务器
├── web/                   # 前端项目
│   ├── src/
│   ├── public/
│   └── package.json
├── migrations/            # 数据库迁移
├── docs/                  # 文档
├── config.yaml            # 配置文件
├── go.mod                 # Go 模块
└── README.md              # 本文件
```

## 开发指南

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行测试并查看覆盖率
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.txt ./...
go tool cover -html=coverage.txt
```

### 代码规范

- 遵循 Go 官方代码规范
- 使用 `gofmt` 格式化代码
- 使用 `golint` 检查代码质量
- 单元测试覆盖率 ≥ 70%

### 安全扫描

```bash
# 安装安全扫描工具
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

# 运行安全扫描
gosec ./...
govulncheck ./...
```

## API 文档

### 认证接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/register | 用户注册 |
| POST | /api/v1/login | 用户登录 |
| POST | /api/v1/token/refresh | 刷新 Token |

### 任务接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tasks | 创建下载任务 |
| POST | /api/v1/tasks/batch | 批量创建任务 |
| GET | /api/v1/tasks | 获取任务列表 |
| GET | /api/v1/tasks/:id | 获取任务详情 |
| DELETE | /api/v1/tasks/:id | 取消任务 |

### WebSocket

```
ws://localhost:8080/ws?token=<jwt_token>
```

## 部署

### 编译

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o bin/collector cmd/server/main.go

# Windows
GOOS=windows GOARCH=amd64 go build -o bin/collector.exe cmd/server/main.go
```

### Docker

```bash
# 构建镜像
docker build -t collector:latest .

# 运行容器
docker run -d -p 8080:8080 -v ./data:/app/data collector:latest
```

## 许可证

MIT License

## 贡献指南

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 联系方式

- 项目负责人: [待定]
- 技术支持: [待定]
