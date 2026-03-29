# CGO 环境部署指南

## 📋 概述

本项目使用 `go-sqlite3` 作为数据库驱动，该驱动依赖 CGO 才能正常工作。如果需要使用 CGO 模式编译（获得更好的 SQLite 性能），需要安装 C 编译器。

---

## 🔧 Windows 环境部署

### 方案一：安装 MinGW-w64（推荐）

1. **下载 MinGW-w64**
   - 访问：https://www.mingw-w64.org/
   - 或使用 winget 安装：
     ```powershell
     winget install MSYS2.MSYS2
     ```

2. **安装步骤**
   ```powershell
   # 使用 MSYS2 安装
   pacman -S mingw-w64-x86_64-gcc
   ```

3. **配置环境变量**
   - 将 MinGW 的 bin 目录添加到 PATH
   - 例如：`C:\msys64\mingw64\bin`

4. **验证安装**
   ```cmd
   gcc --version
   ```

### 方案二：使用 TDM-GCC

1. **下载 TDM-GCC**
   - 访问：https://jmeubank.github.io/tdm-gcc/
   - 下载并安装 TDM64-GCC

2. **配置环境变量**
   - 安装时自动添加到 PATH
   - 或手动添加：`C:\TDM-GCC-64\bin`

3. **验证安装**
   ```cmd
   gcc --version
   ```

### 方案三：使用 Visual Studio Build Tools

1. **下载 Visual Studio Build Tools**
   - 访问：https://visualstudio.microsoft.com/downloads/
   - 选择 "Build Tools for Visual Studio"

2. **安装 C++ 组件**
   - 运行安装程序
   - 选择 "使用 C++ 的桌面开发" 工作负载
   - 安装

3. **配置环境变量**
   ```cmd
   "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat"
   ```

---

## 🐧 Linux 环境部署

### Ubuntu/Debian

```bash
# 安装构建工具
sudo apt-get update
sudo apt-get install build-essential

# 验证安装
gcc --version
```

### CentOS/RHEL

```bash
# 安装开发工具组
sudo yum groupinstall "Development Tools"

# 或单独安装
sudo yum install gcc gcc-c++ make

# 验证安装
gcc --version
```

### Fedora

```bash
sudo dnf groupinstall "Development Tools"
sudo dnf install gcc gcc-c++
```

---

## 🍎 macOS 环境部署

```bash
# 安装 Xcode Command Line Tools
xcode-select --install

# 验证安装
gcc --version
```

---

## 📦 使用 CGO 编译项目

安装 CGO 环境后，可以使用以下命令编译：

### Windows

```cmd
# 设置环境变量
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64

# 编译
go build -ldflags="-s -w" -o collector.exe ./cmd/server
```

### Linux

```bash
# 设置环境变量
export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

# 编译
go build -ldflags="-s -w" -o collector ./cmd/server
```

---

## ⚠️ 注意事项

1. **CGO_ENABLED=0**：当前项目使用 `CGO_ENABLED=0` 编译，使用纯 Go 模式，无需 CGO 环境
2. **性能影响**：CGO 模式提供更好的 SQLite 性能，但需要 C 编译器
3. **跨平台编译**：使用 `CGO_ENABLED=0` 可以跨平台编译，但会失去一些 SQLite 优化
4. **生产环境**：建议在生产环境使用 CGO 模式获得最佳性能

---

## 🔍 验证 CGO 状态

```bash
# 检查 CGO 是否启用
go env CGO_ENABLED

# 查看当前 CGO 配置
go env | grep CGO

# 测试编译
go build -v ./cmd/server
```

---

## 📚 参考链接

- [Go CGO 官方文档](https://golang.org/cmd/cgo/)
- [go-sqlite3 文档](https://github.com/mattn/go-sqlite3)
- [MinGW-w64 官网](https://www.mingw-w64.org/)
- [TDM-GCC 官网](https://jmeubank.github.io/tdm-gcc/)

---

> 文档版本：v1.0
> 创建时间：2026-03-29
> 最后更新：2026-03-29
