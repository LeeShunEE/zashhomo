# Contributing to zashhomo

感谢你考虑为 zashhomo 做贡献！

## 开发环境

### 要求

- Go 1.24 或更高版本
- Git

### Fork 并 Clone

```sh
# Fork 仓库后
git clone https://github.com/<你的用户名>/zashhomo.git
cd zashhomo
```

### 构建

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo
```

### 运行测试

```sh
go test -race ./...
```

## 代码规范

### 格式化

确保代码通过 `gofmt` 检查：

```sh
gofmt -l cmd internal
# 无输出表示格式正确
```

### 静态检查

```sh
go vet ./...
```

### 代码风格

- 遵循 [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- 导出的函数/类型需添加注释
- 错误处理不要忽略

## 提交规范

### 提交信息格式

```
<类型>: <简短描述>

[可选的详细描述]
```

类型示例：
- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `refactor`: 重构
- `test`: 测试相关
- `chore`: 构建/工具相关

示例：
```
feat: add config validation on startup

Validate required fields in zashhomo.yaml before starting
the service to provide clearer error messages.
```

### 提交要求

- 一个提交解决一个问题
- 提交信息使用英文
- 确保每次提交都能通过测试和构建

### DCO 签名

本项目要求每个提交包含 **Developer Certificate of Origin (DCO)** 签名。

在提交时添加 `-s` 参数自动签名：

```sh
git commit -s -m "feat: your feature description"
```

这会自动添加如下签名：

```
Signed-off-by: Your Name <your.email@example.com>
```

你也可以配置 Git 自动签名：

```sh
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"
```

## Pull Request 流程

1. **创建分支**
   ```sh
   git checkout -b feat/your-feature
   ```

2. **编写代码并测试**
   ```sh
   go test -race ./...
   go build ./...
   ```

3. **提交变更**
   ```sh
   git add .
   git commit -m "feat: your feature description"
   ```

4. **推送并创建 PR**
   ```sh
   git push origin feat/your-feature
   ```
   然后在 GitHub 上创建 Pull Request。

5. **等待审核**
   - CI 检查必须通过
   - 至少等待一个审核通过

## 目录结构

```
.
├── cmd/zashhomo/      # 主程序入口
├── internal/          # 内部包
│   ├── config/        # 配置处理
│   ├── core/          # 内核管理
│   ├── daemon/        # 守护进程
│   ├── elevate/       # 权限提升
│   ├── ghrelease/     # GitHub Release
│   ├── panel/         # 面板安装
│   ├── paths/         # 路径处理
│   ├── selfinstall/   # 自安装逻辑
│   ├── subscription/  # 订阅管理
│   ├── svc/           # 系统服务
│   ├── ui/            # 交互界面
│   └── web/           # Web 服务器
├── install.sh         # Linux/macOS 安装脚本
├── install.ps1        # Windows 安装脚本
└── go.mod             # 依赖声明
```

## 发布流程（仅维护者）

1. 更新 `RELEASE.md` 中的变更日志
2. 创建版本标签：
   ```sh
   git tag -a v0.x.0 -m "Release v0.x.0"
   git push origin v0.x.0
   ```
3. CI 自动构建并发布到 GitHub Releases

## 问题反馈

- Bug 报告：使用 [GitHub Issues](https://github.com/LeeShunEE/zashhomo/issues)
- 功能请求：同样使用 Issues
- 安全漏洞：请参见 [SECURITY.md](SECURITY.md)

## 许可证

本项目采用 MIT 许可证，贡献的代码将以相同许可证发布。