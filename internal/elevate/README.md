# Windows 服务安装权限修复

## 修改概述

为 `zashhomo install` 和 `zashhomo uninstall` 命令添加了 Windows 管理员权限检测和自动提升功能。

## 实现的文件

### 新建文件

1. **`internal/elevate/elevate_windows.go`**
   - `IsAdmin() bool` - 使用 Windows API `IsUserAnAdmin()` 检测管理员权限
   - `RunElevated(args []string) error` - 使用 `ShellExecuteW` 的 `runas` 动词触发 UAC 提升

2. **`internal/elevate/elevate_other.go`**
   - 非 Windows 平台的空实现（直接返回 `true`）

3. **`internal/elevate/elevate_windows_test.go`**
   - 基本的单元测试

### 修改文件

1. **`cmd/zashhomo/main.go`**
   - 添加了 `internal/elevate` 包的导入
   - 在 `cmdInstall()` 函数开头添加权限检查和自动提升
   - 在 `cmdUninstall()` 函数开头添加权限检查和自动提升

## 工作流程

### 安装服务 (`zashhomo install`)

1. 用户运行 `zashhomo.exe install [flags]`
2. 程序检测是否具有管理员权限
3. **如果没有管理员权限**：
   - 显示提示信息："Installing the Windows service requires administrator privileges."
   - 显示："Requesting elevation..."
   - 使用 `ShellExecuteW` 的 `runas` 动词重新启动程序
   - Windows 显示 UAC 对话框，用户需要点击"是"授权
   - 原进程退出（无需用户手动重试）
4. **如果已有管理员权限**：
   - 继续正常安装流程

### 卸载服务 (`zashhomo uninstall`)

流程与安装相同，只是提示信息改为："Uninstalling the Windows service requires administrator privileges."

## 测试方法

### 测试场景 1：普通用户权限下安装

```powershell
# 在普通 PowerShell 窗口中运行（非管理员）
.\zashhomo.exe install
```

**预期行为**：
1. 显示提示信息
2. 弹出 UAC 对话框
3. 用户点击"是"后，以管理员权限重新运行安装
4. 安装成功完成

### 测试场景 2：管理员权限下安装

```powershell
# 在管理员 PowerShell 窗口中运行
.\zashhomo.exe install
```

**预期行为**：
1. 直接开始安装（无 UAC 提示）
2. 安装成功完成

### 测试场景 3：用户取消 UAC 提示

```powershell
# 在普通 PowerShell 窗口中运行
.\zashhomo.exe install
# 在 UAC 对话框中点击"否"
```

**预期行为**：
1. 显示提示信息
2. 弹出 UAC 对话框
3. 用户点击"否"取消授权
4. 程序退出，服务未安装

## 技术细节

### Windows API 调用

1. **`IsUserAnAdmin()`** - `shell32.dll`
   - 返回非零值表示当前进程具有管理员权限
   - 替代方案：检查用户的 SID 是否在管理员组中

2. **`ShellExecuteW()`** - `shell32.dll`
   - 使用 `"runas"` 动词触发 UAC 提升
   - 参数：
     - `hwnd`: 0（无父窗口）
     - `lpOperation`: "runas"
     - `lpFile`: 可执行文件路径
     - `lpParameters`: 命令行参数
     - `lpDirectory`: 0（使用可执行文件的目录）
     - `nShowCmd`: SW_SHOWNORMAL (1)

### 参数传递

- 使用 `os.Args[1:]` 传递原始命令行参数
- 使用 `syscall.EscapeArg()` 正确转义参数
- 例如：`install --mixed-port 9190 --web-port 9191`

## 兼容性

- **Windows**: 自动 UAC 提升
- **Linux/macOS**: 直接返回 `true`（使用 `sudo` 或 `root` 用户）
- **构建标签**: 使用 `//go:build windows` 和 `//go:build !windows` 确保正确的平台编译

## 相关文件

- [`install.ps1`](../../install.ps1) - PowerShell 安装脚本（已实现类似的 UAC 检测和提升逻辑）
- [`internal/svc/svc.go`](../svc/svc.go) - 服务管理包装器

## 未来改进

1. **start/stop/restart 命令**: 考虑是否也需要添加权限检查
2. **错误处理**: 提供更详细的错误信息（例如，如何以管理员身份运行）
3. **日志记录**: 记录权限提升过程（用于调试）