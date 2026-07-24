# zashhomo

A lightweight cross-platform [mihomo](https://github.com/MetaCubeX/mihomo) daemon/manager with built-in [zashboard](https://github.com/Zephyruso/zashboard) web panel. A single static binary under 15MB, one command to install kernel + panel and keep it running as a daemon.

![](assets/image.png)

## Features

- **Process Daemon**: Start, health check (Clash `/version`), crash recovery with exponential backoff auto-restart (1s→30s).
- **Auto Download/Update Kernel**: Automatically select mihomo assets by platform (amd64 defaults to `compatible` package).
- **Built-in Panel Hosting + Unified Secret**: Hosts zashboard as HTTP static site and reverse-proxies Clash REST API. The same auto-generated secret protects both mihomo API (written to kernel config, controller loopback-only) and panel access (unlock via token URL first time, then cookie-based).
- **Subscription/Config Management**: Merges multiple subscriptions as mihomo `proxy-providers`, scheduled refresh with hot reload.
- **System Service**: Unified wrapper for systemd / launchd / Windows services via [kardianos/service](https://github.com/kardianos/service), auto-start on boot.

## One-Line Install

Linux / macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.ps1 | iex
```

After installation, `zashhomo status` prints a panel URL with token (e.g. `http://127.0.0.1:9191/?token=<secret>`), open in browser to auto-login to zashboard panel. Default loopback-only, see "Access & Security" below for remote access.

## Commands

```
zashhomo install [--mixed-port N] [--web-port N] [--web-addr ADDR] [--force]
                              Download kernel+panel → generate default config → register system service → start
                              (prompts if service exists; --force to replace)
zashhomo run [--mixed-port N] [--web-port N] [--web-addr ADDR]
                              Run daemon in foreground (called by service)
zashhomo -i | interactive     Interactive management menu (arrow keys; falls back to line input in non-TTY)
zashhomo service start|stop|restart|status   Control installed service (start/stop/restart require admin)
zashhomo status               Show service status
zashhomo dashboard            Open the zashboard panel in your default browser (token auto-login)
zashhomo onboard              Guided setup: install, subscribe, restart, system proxy, panel
zashhomo system-proxy enable|disable   Set/clear the OS system proxy (points at mixed-port)
zashhomo update [--core|--ui|--self|--all]   Update components
zashhomo sub add <url> [name] Add a subscription and download it into the cache
zashhomo sub list             List subscriptions with their state (▸ marks the active one)
zashhomo sub show <index>     Show one subscription in full
zashhomo sub switch <index>   Make that subscription the active profile (reads the cache)
zashhomo sub update [index]   Refresh one subscription, or every enabled one
zashhomo sub enable|disable <index>
                              Resume or pause a subscription (paused ones never refresh)
zashhomo sub auto <index> on|off
                              Turn that subscription's scheduled update on or off
zashhomo sub interval [dur]   Show or set the global refresh interval (e.g. 6h, 30m)
zashhomo sub interval <index> <dur>
                              Give one subscription its own interval ('default' to clear)
zashhomo sub remove <index>   Remove the subscription at <index> (see 'sub list')
zashhomo sub edit             Open the config file in your editor
zashhomo uninstall [--purge]  Stop service and remove (--purge also deletes data/config)
zashhomo version              Print version
```

`--mixed-port` specifies mihomo mixed proxy port (default 9190, effective on mihomo start);
`--web-port` only changes panel port, keeps listen host (default `127.0.0.1:9191`);
`--web-addr` specifies full listen address (`host:port`), e.g. `0.0.0.0:9191` for public exposure.
These are persisted to `zashhomo.yaml`.

### Add Subscription

```sh
zashhomo sub add https://example.com/your-subscription
```

Subscriptions follow a **profile model**: each one is downloaded verbatim into
`<data dir>/subs/<id>.yaml`, but only **one** is active at a time — `config.yaml`
is generated purely from the active subscription (its own proxies, proxy groups
and rules), while the others sit idle in the cache.

```sh
zashhomo sub list             # see them all; ▸ marks the active one
zashhomo sub switch 1         # activate #1 (reads the cache, works offline)
zashhomo sub auto 1 off       # turn off scheduled updates for that one only
zashhomo sub interval 1 30m   # give that one its own 30-minute interval
```

Disabling a subscription means it can neither be switched to nor refreshed on a
schedule; disabling the active one automatically switches to the next enabled
subscription. Switching and refreshing both hot-reload the kernel.

The interactive menu (`zashhomo -i`) shows `Current active:` at the top of its
Subscriptions page, with arrow-key entries for switching the active profile and
managing each subscription individually.

## Directory Layout

| Platform | Data Directory | Config |
|----------|----------------|--------|
| Linux/macOS (user) | `~/.local/share/zashhomo` | `~/.config/zashhomo/zashhomo.yaml` |
| Linux/macOS (root) | `/var/lib/zashhomo` | `/etc/zashhomo/zashhomo.yaml` |
| Windows | `%ProgramData%\zashhomo` | Same as data directory |

Data directory contains: `bin/` (mihomo binary), `ui/` (zashboard static), `subs/` (raw subscription files, named by id), `providers/` (mihomo's own provider cache), `config.yaml` (mihomo config generated from the active subscription), `zashhomo.log`.

Environment variables: `ZASHHOMO_DATA`, `ZASHHOMO_CONFIG_DIR`.

## Configuration (`zashhomo.yaml`)

```yaml
controller_addr: 127.0.0.1:9090   # mihomo external-controller (loopback only)
secret: <auto-generated>           # Protects both Clash API and panel access
web_addr: 127.0.0.1:9191          # Panel + API reverse-proxy listen address (default loopback)
mixed_port: 9190                  # mihomo mixed proxy port (can be changed via --mixed-port)
sub_interval: 12h                 # Global subscription refresh interval
active_sub: a1b2c3d4e5f60718      # id of the active subscription (kept by 'sub switch')
subscriptions:                    # Subscription list
  - id: a1b2c3d4e5f60718          #   stable id, also the cache file name (auto-generated)
    name: Provider A              #   display name
    url: https://example.com/sub  #   subscription URL
    disabled: false               #   true: paused — cannot be activated, never refreshes
    no_auto_update: false         #   true: skip this one during scheduled refreshes
    interval: 30m                 #   per-subscription interval (blank follows sub_interval)
    updated_at: 2026-07-24T10:00:00Z  # last successful refresh (auto-recorded)
core_version: ""                  # Installed kernel version (auto-recorded)
ui_version: ""                    # Installed panel version (auto-recorded)
```

## Access & Security

- **Single Secret**: zashhomo auto-generates a 128-bit secret (stored in `zashhomo.yaml`, 0600), used for both mihomo's Clash API key and web panel access credential. No manual config needed.
- **Default Loopback Only**: Web panel defaults to `127.0.0.1:9191`, mihomo controller to `127.0.0.1:9090`, both localhost-only.
- **Open Panel**: `zashhomo status` gives token URL `http://127.0.0.1:9191/?token=<secret>`, browser opens to auto-login; direct access shows login page, enter secret to unlock. API clients use `Authorization: Bearer <secret>`.
- **Access from External Devices**: Default loopback, external can't connect directly. Two options:
  - **SSH Port Forward (Recommended, Zero Exposure)**: `ssh -L 9191:127.0.0.1:9191 user@host`, then open panel URL in local browser. No config change, secret never leaves the host.
  - **Public Listen**: `zashhomo install --web-addr 0.0.0.0:9191` (or change `zashhomo.yaml` `web_addr` to `0.0.0.0:9191` then `restart`). Secret gate is the only protection—keep it safe. For public internet, recommend reverse proxy/TLS. Then open `http://<host-ip>:9191/?token=<secret>` from external device.

## Build from Source

Requires Go 1.23+:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o zashhomo ./cmd/zashhomo
```

Cross-compile example:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags "-s -w -X main.version=v0.1.0" -o zashhomo-v0.1.0-linux-arm64 ./cmd/zashhomo
```

## Design Notes

- Only depends on stdlib + `gopkg.in/yaml.v3` + `github.com/kardianos/service`, no heavy frameworks like cobra; CLI subcommands are hand-written dispatch, `CGO_ENABLED=0` for static binary + `-ldflags "-s -w"` for size reduction.
- Kernel and panel both default to loopback; panel and Clash API share the same secret, reverse proxy injects it, credentials never leak.

## Release

Pushing `v*` tag triggers `.github/workflows/release.yml`: cross-compiles linux/darwin(amd64/arm64) and windows(amd64/arm64), uploads `zashhomo-<version>-<os>-arch>[.exe]` + `SHA256SUMS.txt` to Releases.

Repository: [github.com/LeeShunEE/zashhomo](https://github.com/LeeShunEE/zashhomo).