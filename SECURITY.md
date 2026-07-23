# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | ✅ |
| < 1.0   | ❌ |

## Reporting a Vulnerability

**请勿在公开 Issue 中报告安全漏洞。**

请使用 GitHub Security Advisories 私密报告：

1. 访问 [Security Advisories](https://github.com/LeeShunEE/zashhomo/security/advisories)
2. 点击 "Report a vulnerability"
3. 填写漏洞详情

### 报告内容

- 漏洞描述
- 复现步骤
- 影响范围
- 可能的修复建议（如有）

### 响应承诺

- **确认时间**：3 个工作日内确认收到报告
- **评估时间**：7 个工作日内评估漏洞严重程度
- **修复时间**：根据严重程度，尽快发布修复版本

### 披露政策

- 未经报告者同意，不会公开披露漏洞详情
- 修复发布后，会在 Release Notes 中致谢报告者（如愿意）

## 安全建议

### 生产环境部署

- 不要在公网直接暴露面板端口
- 使用 SSH 端口转发访问面板
- 如需公网访问，请配置 TLS 反向代理
- 定期更新到最新版本

### 密钥管理

- `zashhomo.yaml` 包含自动生成的 secret，请妥善保管
- 不要将配置文件提交到公开仓库
- 定期更换 secret（手动修改后重启服务）