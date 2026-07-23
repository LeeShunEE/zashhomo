# zashhomo

轻量化的跨平台 [mihomo](https://github.com/MetaCubeX/mihomo) 守护 / 管理程序，内置
[zashboard](https://github.com/Zephyruso/zashboard) Web 面板。一个 <15MB 的静态单二进制，
一行命令即可装好内核 + 面板并常驻守护。

## 特性

- **进程守护**：启动、健康检查（Clash `/version`）、崩溃指数退避自动重启（1s→30s）。
- **自动下载 / 更新内核**：按平台自动选择 mihomo 资产（amd64 默认取 `compatible` 兼容包）。
- **内置面板托管**：自带 HTTP 静态站托管 zashboard，并反向代理 Clash REST API（自动注入 secret）。
  打开一个地址即用，无需手填 secret；内核只监听 `127.0.0.1:9090`，对外仅暴露面板端口。
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

安装完成后打开 `http://<主机>:9191` 即为 zashboard 面板。

## 命令

```
zashhomo install [--mixed-port N] [--web-port N]
                              下载内核+面板 → 生成默认配置 → 注册系统服务 → 启动
zashhomo run [--mixed-port N] [--web-port N]
                              前台运行守护（服务调用此命令）
zashhomo -i | interactive     交互式管理控制台（循环输入子命令）
zashhomo start|stop|restart   控制已安装的服务
zashhomo status               查看服务状态
zashhomo update [--core|--ui|--self|--all]   更新组件
zashhomo sub add <url>        添加订阅
zashhomo sub update           重新生成配置并热重载内核
zashhomo uninstall [--purge]  停服务并移除（--purge 连同数据/配置一起删）
zashhomo version              打印版本
```

`--mixed-port` 指定 mihomo 混合代理端口（默认 9190，启动 mihomo 时生效），
`--web-port` 指定面板端口（默认 9191）；指定后会写入并持久化到 `zashhomo.yaml`。

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
secret: <自动生成>                 # Clash API 密钥
web_addr: 0.0.0.0:9191            # 面板 + API 反代监听地址（可用 --web-port 改）
mixed_port: 9190                  # mihomo 混合代理端口（可用 --mixed-port 改）
sub_interval: 12h                 # 订阅刷新间隔
subscriptions: []                 # 订阅列表
core_version: ""                  # 已装内核版本（自动记录）
ui_version: ""                    # 已装面板版本（自动记录）
```

## 从源码构建

需要 Go 1.23+：

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo
```

交叉编译示例：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-s -w -X main.version=v0.1.0" -o zashhomo-linux-arm64 ./cmd/zashhomo
```

## 设计说明

- 仅依赖标准库 + `gopkg.in/yaml.v3` + `github.com/kardianos/service`，无 cobra 等重型框架；
  CLI 子命令为手写 dispatch，`CGO_ENABLED=0` 全静态 + `-ldflags "-s -w"` 瘦身。
- 内核只在回环地址暴露控制端口，凭据由面板反代统一注入，避免密钥外泄。

## 发布

推送 `v*` tag 触发 `.github/workflows/release.yml`：交叉编译 linux/darwin(amd64/arm64)
与 windows/amd64，产物 `zashhomo-<os>-<arch>[.exe]` + `SHA256SUMS.txt` 上传至 Releases。
仓库地址：[github.com/LeeShunEE/zashhomo](https://github.com/LeeShunEE/zashhomo)。
