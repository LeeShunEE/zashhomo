# CLAUDE.md - AI 编码代理规范

> 本文件为 Claude Code 等 AI 编码代理提供项目上下文和编码规范。

## 项目概述

zashhomo 是一个轻量级跨平台 mihomo 守护/管理程序，使用 Go 语言编写。

- **语言**: Go 1.24+
- **构建**: 静态单二进制，无 CGO 依赖
- **主要依赖**: `gopkg.in/yaml.v3`, `github.com/kardianos/service`, `github.com/charmbracelet/bubbletea`

## 目录结构

```
.
├── cmd/zashhomo/          # 主程序入口
│   ├── main.go            # CLI 入口
│   ├── menu.go            # 交互式菜单
│   └── theme.go           # UI 主题
├── internal/              # 内部包（不对外暴露）
│   ├── config/            # 配置解析
│   ├── core/              # mihomo 内核管理（平台相关）
│   ├── daemon/            # 守护进程逻辑
│   ├── elevate/           # 权限提升（Windows 特殊处理）
│   ├── ghrelease/         # GitHub Release 下载
│   ├── panel/             # zashboard 面板安装
│   ├── paths/             # 跨平台路径处理
│   ├── selfinstall/       # 自安装/卸载逻辑
│   ├── subscription/      # 订阅管理
│   ├── svc/               # 系统服务封装
│   ├── ui/                # TUI 界面（bubbletea）
│   └── web/               # Web 服务器
├── install.sh             # Linux/macOS 安装脚本
├── install.ps1            # Windows 安装脚本
└── .github/workflows/     # CI/CD 工作流
```

## 编码规范

### Go 代码风格

1. **格式化**: 必须通过 `gofmt` 检查
2. **静态检查**: 必须通过 `golangci-lint`
3. **命名**:
   - 包名：小写单词，不使用下划线
   - 导出函数/类型：必须有文档注释
   - 接口：以 `-er` 结尾（如 `Runner`, `Reader`）

4. **错误处理**:
   - 不要忽略错误
   - 使用 `fmt.Errorf` 包装错误上下文
   - 自定义错误类型放在对应包内

5. **平台相关代码**:
   - 使用文件后缀区分：`_windows.go`, `_linux.go`, `_darwin.go`, `_other.go`
   - 公共接口放在无后缀文件中

### 平台差异处理

```go
// 公共接口 (child.go)
package core

func Start() error { return start() }

// 平台实现 (child_windows.go)
package core

func start() error { /* Windows 实现 */ }

// 平台实现 (child_linux.go)
package core

func start() error { /* Linux 实现 */ }
```

### 配置管理

- 配置文件：`zashhomo.yaml`（用户配置）、`config.yaml`（生成的 mihomo 配置）
- 使用 `internal/config` 包统一管理
- 敏感信息（secret）使用 YAML string 类型，不要打印到日志

### 日志规范

- 日志文件：`<数据目录>/zashhomo.log`
- 格式：`[时间] [级别] 消息`
- 不要在日志中输出敏感信息（secret、订阅 URL 等）

## 测试规范

### 测试文件

- 文件名：`<原文件名>_test.go`
- 位置：与源文件同目录

### 运行测试

```bash
# 运行所有测试
go test -race ./...

# 运行带覆盖率
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# 运行单个包测试
go test -race ./internal/config/...
```

### 测试要求

- 新功能必须添加单元测试
- 覆盖率门槛：30%（逐步提高）
- 平台相关代码：使用条件编译或 mock

### 测试模式

```go
// 表格驱动测试
func TestParseConfig(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    Config
        wantErr bool
    }{
        {"valid", "valid.yaml", Config{...}, false},
        {"invalid", "bad.yaml", Config{}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

## 构建与发布

### 本地构建

```bash
# 开发构建
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo

# 带版本信息
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=v0.1.0" -o zashhomo ./cmd/zashhomo
```

### 交叉编译

```bash
# Linux ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo

# Windows AMD64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o zashhomo.exe ./cmd/zashhomo
```

### 发布流程

1. 更新 `RELEASE.md` 变更日志
2. 创建 tag：`git tag -a v0.x.0 -m "Release v0.x.0"`
3. 推送 tag：`git push origin v0.x.0`
4. CI 自动构建并发布到 GitHub Releases

## 常见任务

### 添加新 CLI 子命令

1. 在 `cmd/zashhomo/main.go` 的 `run()` 函数添加命令分发
2. 实现命令逻辑（放在 `internal/` 对应包）
3. 添加测试
4. 更新 README.md 命令列表

### 添加新配置项

1. 在 `internal/config/config.go` 的 `Config` 结构体添加字段
2. 更新 `Load()` 函数处理新字段
3. 更新 `zashhomo.yaml` 文档
4. 添加配置验证（如有必要）

### 添加新平台支持

1. 创建 `_<platform>.go` 文件
2. 实现必要接口
3. 更新 `install.sh` / `install.ps1` 脚本
4. 更新 CI 交叉编译矩阵

## DCO 签名要求

每个 commit 必须包含 `Signed-off-by` 签名：

```bash
git commit -s -m "feat: add new feature"
```

## 相关文档

- [README.md](README.md) - 项目介绍（中文）
- [README.en.md](README.en.md) - 项目介绍（英文）
- [CONTRIBUTING.md](CONTRIBUTING.md) - 贡献指南
- [SECURITY.md](SECURITY.md) - 安全策略