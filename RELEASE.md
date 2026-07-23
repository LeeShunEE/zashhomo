# 版本发布流程

本文档说明如何发布新版本。

## 更新日志

### Unreleased

**新功能：system-proxy**
- 新增 `zashhomo system-proxy enable|disable` 命令，一键开启/关闭系统代理（指向 mixed-port）
- 跨平台实现：Windows 写 WinINET 注册表并广播刷新；macOS 调用 `networksetup`；Linux 使用 GNOME `gsettings`
- `zashhomo status` 与交互式菜单展示系统代理状态
- 新增配置项 `system_proxy`；启用后守护进程启动时自动开启、停止时自动关闭

### v0.3.0 (2025-07-23)

**开源仓库完善**
- 添加 MIT 许可证
- 添加英文版 README (README.en.md)
- 添加贡献指南 (CONTRIBUTING.md)
- 添加安全策略 (SECURITY.md)
- 添加行为准则 (CODE_OF_CONDUCT.md)
- 添加 AI 协作规范 (CLAUDE.md)
- 添加 Issue 模板 (Bug 报告、功能请求)
- 添加 PR 模板

**CI/CD 增强**
- 添加 DCO 签名校验工作流
- 添加 golangci-lint 静态分析
- 添加测试覆盖率门禁 (25%)
- 恢复 Linux/macOS 平台交叉编译

**测试补充**
- 新增 8 个测试文件
- 覆盖率从 15.9% 提升到 29.9%
- 修复 Windows 平台编译问题

### v0.2.0 (2025-07-22)

- 添加交互式菜单（方向键选择）
- 添加统一主题系统
- 显示服务状态和 ASCII banner
- 修复 Windows 服务状态检测

### v0.1.0 (2025-07-21)

- 首个发布版本
- 基本的 mihomo 守护功能
- zashboard 面板集成
- 系统服务支持

---

## 工作流触发机制

本项目使用两个独立的工作流：

| 工作流 | 触发条件 | 运行内容 |
|--------|----------|----------|
| **CI** | 推送到 `main` 分支<br>Pull Request<br>手动触发 | ✅ 代码格式检查 (`gofmt`)<br>✅ 静态分析 (`go vet`)<br>✅ 单元测试 (`go test -race`)<br>✅ 交叉编译验证 |
| **Release** | 推送 `v*` 标签<br>手动触发 | ✅ 构建发布产物<br>✅ 上传 Artifacts<br>✅ 创建 GitHub Release<br>✅ 生成 SHA256 校验和 |

> **关键点**：推送标签时只触发 **Release** 工作流，不会重复运行 CI 测试。

## 发版流程

### 1. 确保代码已合并到 main
```bash
git checkout main
git pull origin main
```

### 2. 验证 CI 通过
推送代码到 `main` 后，CI 工作流会自动运行：
- 格式检查
- 静态分析
- 单元测试
- 交叉编译验证

在 GitHub Actions 页面确认所有检查通过后再继续。

### 3. 更新版本信息（可选）
如有需要，更新 README.md、CHANGELOG.md 等文档。

### 4. 创建版本标签
```bash
# 创建带注释的标签（推荐）
git tag -a v1.0.0 -m "Release v1.0.0: 简短描述"

# 或创建轻量级标签
git tag v1.0.0
```

### 5. 推送标签到远程
```bash
git push origin v1.0.0
```

### 6. 自动发布
推送标签后，GitHub Actions Release 工作流会自动：
1. 构建 Windows 平台二进制文件（amd64 + arm64）
2. 生成 SHA256 校验和
3. 创建 GitHub Release 并上传所有产物

## 版本命名规范

遵循 [语义化版本](https://semver.org/lang/zh-CN/)：

- **主版本号 (MAJOR)**：不兼容的 API 修改
- **次版本号 (MINOR)**：向后兼容的功能新增
- **修订号 (PATCH)**：向后兼容的问题修正

示例：
- `v1.0.0` - 首个正式版本
- `v1.1.0` - 新增功能
- `v1.1.1` - Bug 修复

## 发布产物

每个版本会发布以下文件（当前仅支持 Windows 平台）：

```
zashhomo-v1.0.0-windows-amd64.exe
zashhomo-v1.0.0-windows-arm64.exe
SHA256SUMS.txt
```

> **注意**：Linux 和 macOS 平台暂时禁用。如需启用，请编辑 `.github/workflows/release.yml` 和 `.github/workflows/ci.yml`，取消相应平台的注释即可。

## 校验下载

下载后验证完整性：
```bash
# Linux/macOS
sha256sum -c SHA256SUMS.txt

# Windows (PowerShell)
# 注意替换版本号和架构
Get-FileHash zashhomo-v1.0.0-windows-amd64.exe -Algorithm SHA256
```

## 手动触发发布

如需手动触发（不推荐），可在 GitHub 仓库的 Actions 页面手动运行 `release` workflow。

## 回滚发布

如发现问题需要回滚：
```bash
# 删除远程标签
git push --delete origin v1.0.0

# 删除本地标签
git tag -d v1.0.0

# 删除 GitHub Release（需在网页操作）
```

## CI/CD 检查项

每次发布前确保：
- [ ] 所有测试通过 (`go test -race ./...`)
- [ ] 代码格式正确 (`gofmt -l cmd internal`)
- [ ] 静态分析通过 (`go vet ./...`)
- [ ] 版本号已更新（如有需要）
- [ ] CHANGELOG 已更新（如有需要）