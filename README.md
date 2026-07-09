# Clash Jump Manager

Clash Jump Manager 是一个 Windows 本地 Web 工具，用来管理 Clash Verge Rev / Mihomo 的 `dialer-proxy` 覆盖脚本。

它的目标很简单：当某个订阅节点服务器在当前网络不可达时，选择另一个可用订阅节点作为跳板，并为目标订阅生成或更新覆盖脚本，让目标节点通过跳板节点拨出。

程序打包后是单个 Windows exe。启动后会在本机监听 `127.0.0.1:8766`，Web UI 使用 Go `embed.FS` 内嵌，不依赖全局 Python 或 Node.js。

## 与 Clash Verge Rev 的关系

本项目是面向 [Clash Verge Rev](https://github.com/clash-verge-rev/clash-verge-rev) / Mihomo 配置文件的非官方辅助工具。

本项目不属于 Clash Verge Rev 官方项目，也未获得其官方背书、赞助或维护。它只是读取本机 Clash Verge Rev 配置目录，并在用户确认后管理 profile script 中的 `dialer-proxy` 覆盖脚本。

## 功能

- 读取 Clash Verge Rev 的 `profiles.yaml`、订阅缓存 YAML 和订阅脚本。
- 从现有订阅节点中选择跳板源节点。
- 为目标订阅生成 `dialer-proxy` 覆盖脚本。
- 写入前预览脱敏后的脚本。
- 覆盖已有脚本前自动备份。
- 检测当前实际脚本状态。
- 区分本工具生成的脚本和第三方或手工写入的跳板脚本。
- 只重置本工具生成或接管管理过的脚本。
- 本地状态不保存节点密码、UUID 等敏感字段。

## 安全原则

这个工具会在用户确认后修改 Clash Verge Rev 的 profile script 文件，因此安全边界比功能本身更重要。

- 写入前必须先预览。
- 覆盖已有脚本前必须备份。
- reset 只清理本工具生成或管理过的脚本。
- `state.json` 不保存节点密码、UUID、`ws-opts`、`reality-opts`、`plugin-opts` 等敏感字段。
- 真正写入时，才从本机 Clash 订阅缓存中读取节点密钥。
- 服务只监听 `127.0.0.1`。
- 不替代 Clash Verge Rev、Mihomo 或系统代理。

Clash Verge Rev 配置目录通常是：

```text
%APPDATA%\io.github.clash-verge-rev.clash-verge-rev
```

关键文件通常包括：

```text
profiles.yaml
profiles/<uid>.yaml
profiles/<script_uid>.js
```

## 下载与运行

1. 从 GitHub Releases 下载 Windows 压缩包。
2. 解压到一个可写目录，例如桌面、下载目录或自己的工具目录。
3. 双击 `clash-jump-manager.exe`。
4. 如果浏览器没有自动打开，手动访问 `http://127.0.0.1:8766/`。

便携版会在 exe 同目录写入 `state.json` 和 `backups/`。不建议放到 `C:\Program Files` 这类默认不可写目录。

## 从源码运行

需要：

- Windows
- Go 1.26 或更新版本

本地运行：

```powershell
go run ./cmd/clash-jump-manager
```

构建 exe：

```powershell
go build -o dist/clash-jump-manager.exe ./cmd/clash-jump-manager
```

运行测试：

```powershell
go test ./...
```

如果默认 Go module proxy 访问不稳定，可以临时使用：

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

## 项目结构

```text
cmd/clash-jump-manager/      # 程序入口和内嵌 Web UI
internal/clash/              # Clash Verge 配置、订阅、节点缓存读取
internal/jump/               # 匹配规则、脚本生成、脚本检测
internal/store/              # 本地状态、备份、reset 管理边界
internal/server/             # HTTP API 和静态 Web 服务
static/                      # 原生 HTML/CSS/JS 参考副本
```

## API 简述

主要读取接口：

```text
GET    /api/subscriptions
GET    /api/subscriptions/{uid}/nodes
GET    /api/jump/state
GET    /api/jump/preview
POST   /api/jump/preview
GET    /api/runtime/status
GET    /api/diagnostics
```

写入接口：

```text
POST   /api/apply
POST   /api/reset
POST   /api/scripts/{uid}/disable-foreign-jump
```

旧版状态修改接口仍保留用于兼容，但当前 Web UI 会先把修改保存在本地草稿中，只有预览和应用配置时才进入真实写入流程。

## 发布包注意事项

便携版发布包只应包含：

```text
clash-jump-manager.exe
README.txt
```

不要发布：

```text
state.json
backups/
dist/
本机生成的 *.zip
Clash Verge 订阅缓存文件
```

备份文件和订阅缓存可能包含真实节点密钥。

## License

MIT
