# zashhomo

轻量化的跨平台 [mihomo](https://github.com/MetaCubeX/mihomo) 守护 / 管理程序，内置
[zashboard](https://github.com/Zephyruso/zashboard) Web 面板。一个 <15MB 的静态单二进制，
一行命令即可装好内核 + 面板并常驻守护。

![](assets/image.png)

## 特性

- **进程守护**：启动、健康检查（Clash `/version`）、崩溃指数退避自动重启（1s→30s）。
- **自动下载 / 更新内核**：按平台自动选择 mihomo 资产（amd64 默认取 `compatible` 兼容包）。
- **内置面板托管 + 统一密钥**：自带 HTTP 静态站托管 zashboard，并反向代理 Clash REST API。
  同一个自动生成的 secret 既保护 mihomo API（写入内核配置、控制器仅回环），也作为面板访问凭证
  （首次用带 token 的 URL 解锁、之后靠 cookie）。
- **订阅 / 配置管理**：以 mihomo `proxy-providers` 方式合并多个订阅，定时刷新并热重载。
- **系统服务**：通过 [kardianos/service](https://github.com/kardianos/service) 统一封装
  systemd / launchd / Windows 服务，开机自启。

## 一行安装

Linux / macOS：

```sh
curl -fsSL https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.sh | bash
```

Windows（PowerShell）：

```powershell
irm https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.ps1 | iex
```

安装完成后，`zashhomo status` 会打印一个带 token 的面板地址（形如
`http://127.0.0.1:9191/?token=<secret>`），浏览器打开即自动登录为 zashboard 面板。默认仅监听
回环，远程访问见下文「访问与安全」。

## 命令

```
zashhomo install [--mixed-port N] [--web-port N] [--web-addr ADDR] [--force]
                              下载内核+面板 → 生成默认配置 → 注册系统服务 → 启动
                              （服务已存在时会提示是否替换；--force 直接强制替换）
zashhomo run [--mixed-port N] [--web-port N] [--web-addr ADDR]
                              前台运行守护（服务调用此命令）
zashhomo -i | interactive     交互式管理菜单（方向键选择命令；非终端下回退为逐行输入）
zashhomo service start|stop|restart|status   控制已安装的服务（start/stop/restart 需管理员）
zashhomo status               查看服务状态
zashhomo dashboard            用默认浏览器打开 zashboard 面板（自动带 token 登录）
zashhomo onboard              新手引导：装服务 → 加订阅 → 重启 → 开系统代理 → 开面板
zashhomo system-proxy enable|disable   开启/关闭系统代理（指向 mixed-port）
zashhomo update [--core|--ui|--self|--all]   更新组件
zashhomo sub add <url>        添加订阅
zashhomo sub list             查看订阅（元信息 + 编辑提示）
zashhomo sub edit             用编辑器打开配置文件
zashhomo sub interval [dur]   查看/设置全局刷新间隔（如 6h、30m）
zashhomo sub update           重新生成配置并热重载内核
zashhomo uninstall [--purge]  停服务并移除（--purge 连同数据/配置一起删）
zashhomo version              打印版本
```

`--mixed-port` 指定 mihomo 混合代理端口（默认 9190，启动 mihomo 时生效）；
`--web-port` 只改面板端口、保留监听 host（默认 `127.0.0.1:9191`）；
`--web-addr` 指定完整监听地址（`host:port`），如 `0.0.0.0:9191` 表示对外暴露。
指定后会写入并持久化到 `zashhomo.yaml`。

### 添加订阅

```sh
zashhomo sub add https://example.com/your-subscription
```

订阅会以 `proxy-providers` 形式写入 mihomo 配置，生成 `PROXY`（select）与
`AUTO`（url-test）两个策略组；随后自动热重载，面板即可见节点。

## 目录布局

| 平台            | 数据目录                                   | 自身配置                          |
| --------------- | ------------------------------------------ | --------------------------------- |
| Linux/macOS（用户） | `~/.local/share/zashhomo`                  | `~/.config/zashhomo/zashhomo.yaml` |
| Linux/macOS（root） | `/var/lib/zashhomo`                        | `/etc/zashhomo/zashhomo.yaml`     |
| Windows         | `%ProgramData%\zashhomo`                   | 同数据目录                        |

数据目录内含：`bin/`（mihomo 二进制）、`ui/`（zashboard 静态站）、`providers/`（订阅缓存）、
`config.yaml`（生成的 mihomo 配置）、`zashhomo.log`。

可用环境变量覆盖：`ZASHHOMO_DATA`、`ZASHHOMO_CONFIG_DIR`。

## 配置（`zashhomo.yaml`）

```yaml
controller_addr: 127.0.0.1:9090   # mihomo external-controller（仅回环）
secret: <自动生成>                 # 同时保护 Clash API 与面板访问
web_addr: 127.0.0.1:9191          # 面板 + API 反代监听地址（默认仅回环）
mixed_port: 9190                  # mihomo 混合代理端口（可用 --mixed-port 改）
sub_interval: 12h                 # 订阅刷新间隔
subscriptions: []                 # 订阅列表
core_version: ""                  # 已装内核版本（自动记录）
ui_version: ""                    # 已装面板版本（自动记录）
```

## 访问与安全

- **单一 secret**：zashhomo 自动生成一个 128-bit secret（存于 `zashhomo.yaml`，0600），同时用作
  mihomo 的 Clash API 密钥与 web 面板的访问凭证，无需手动配置。
- **默认仅回环**：web 面板默认 `127.0.0.1:9191`，mihomo 控制器 `127.0.0.1:9090`，都只对本机可见。
- **打开面板**：`zashhomo status` 给出带 token 的地址 `http://127.0.0.1:9191/?token=<secret>`，
  浏览器打开即自动登录；直接访问则弹出登录页，输入 secret 解锁。API 客户端可用
  `Authorization: Bearer <secret>`。
- **从外部设备访问**：默认回环，外部无法直连。两种方式：
  - **SSH 端口转发（推荐，零暴露）**：`ssh -L 9191:127.0.0.1:9191 user@host`，再在本机浏览器打开
    面板地址。无需改任何配置，secret 不离开本机。
  - **对外监听**：`zashhomo install --web-addr 0.0.0.0:9191`（或把 `zashhomo.yaml` 的 `web_addr`
    改为 `0.0.0.0:9191` 后 `restart`）。此时 secret gate 是对外的唯一防护——务必保管好 secret，
    公网建议前置反代/TLS。之后从外部设备打开 `http://<主机IP>:9191/?token=<secret>`。

## 从源码构建

需要 Go 1.23+：

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo
```

交叉编译示例：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-s -w -X main.version=v0.1.0" -o zashhomo-v0.1.0-linux-arm64 ./cmd/zashhomo
```

## 设计说明

- 仅依赖标准库 + `gopkg.in/yaml.v3` + `github.com/kardianos/service`，无 cobra 等重型框架；
  CLI 子命令为手写 dispatch，`CGO_ENABLED=0` 全静态 + `-ldflags "-s -w"` 瘦身。
- 内核与面板默认都只听回环；面板与 Clash API 共用同一 secret，反代统一注入，凭据不外泄。

## 发布

推送 `v*` tag 触发 `.github/workflows/release.yml`：当前仅编译 windows(amd64/arm64)，
产物 `zashhomo-<version>-windows-<arch>.exe` + `SHA256SUMS.txt` 上传至 Releases。
其他平台（Linux/macOS）暂时禁用，后续按需启用。
仓库地址：[github.com/LeeShunEE/zashhomo](https://github.com/LeeShunEE/zashhomo)。
