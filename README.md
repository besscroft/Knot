# Knot

基于 [MyGo](https://mygo.egoist.dev/) 的跨平台资源下载器（Windows / macOS），Go 后端 + React 前端（Vite+ 工具链），系统 WebView 渲染，无 cgo。

## 功能

- HTTP/HTTPS 直链下载：多线程分片（默认 8 段）、断点续传、失败自动重试
- **HLS (m3u8) 下载**：master 自动选最高清晰度、分片并发下载、AES-128 解密、断点续传，完成后自动合并为 MP4（gomedia 纯 Go 转封装，失败时保留 .ts）
- **YouTube 视频下载**：基于 [yt-dlp](https://github.com/yt-dlp/yt-dlp) 实现，粘贴链接或应用内浏览到视频页即可选画质下载（含高帧率标注、仅提取音频）；解析/下载/合流全由 yt-dlp 完成，断点续传走 .part；yt-dlp 缺失时自动下载对应平台构建并校验（仅首次），也可在设置中手动指定路径
- **格式选择与转换**（参考 Downbit）：下载时「保存为」可选 MP4/MKV/MOV/WebM 或提取音频为 M4A/MP3/Opus/FLAC/WAV；容器转换与音频提取均为 FFmpeg 流拷贝，速度极快；FFmpeg 缺失时自动下载对应平台构建并校验（仅首次），也可在设置中手动指定路径；设置里可配置视频/音频默认格式
- 下载队列：并发上限可配、暂停/继续/删除、任务栏进度
- **应用内浏览 + 资源嗅探**：浏览时侧栏保留，站点由无边框子窗口呈现在内容区，右上角浮出嗅探面板（自动收集 fetch/XHR 与 DOM 资源，勾选下载）；下载自动携带站点的 Referer/Cookie/UA 规避防盗链；网页触发的下载自动转入下载器；侧栏「浏览中」状态条或嗅探面板均可退出浏览
- **浏览器插件**（`extension/`，基于 [WXT](https://wxt.dev/)）：在日常浏览器里嗅探网页资源（HLS/视频/音频/文档/图片，webRequest 观察），弹窗勾选或右键一键交给 Knot 下载，自动携带页面 Cookie/Referer；B 站视频页支持清晰度选择 + DASH 双流合流（与应用内一致）
- **下载代理**：支持 http/https/socks5，留空跟随系统/环境代理（仅作用于下载器；内置浏览始终走系统代理——webview 代理由 mygo/平台决定）
- 应用设置：下载目录、分片数、并发数、代理
- 类型安全 IPC：Go 服务方法自动生成 TypeScript 客户端（`frontend/src/mygo.ts`）
- 深浅色跟随系统；单实例锁；Windows 关窗退出 / macOS 关窗驻留

### 已知限制

- 资源嗅探只覆盖顶层 frame（iframe 内资源暂不采集）
- HLS 暂不支持：直播流（无 EXT-X-ENDLIST）、fMP4 分片（EXT-X-MAP）、BYTERANGE、SAMPLE-AES
- YouTube 暂不支持：直播流、播放列表批量下载、需登录的受限视频（Cookies）
- 浏览中的 target=_blank 弹页交给系统默认浏览器打开

## 规划

- **M2-b**：HLS (m3u8) 视频流下载（分片并发 + AES-128 解密 + TS 拼接，可选 ffmpeg 转 MP4）
- **M3**：网页资源批量抓取（goquery 提取图片/视频/附件后勾选下载）
- **M4**：特定站点解析插件（`parser.Register(domain, Parser)` 注册表）

## 开发

环境要求：Go ≥ 1.27、[Bun](https://bun.sh)、平台自带 WebView（Win10+ WebView2 / macOS WKWebView）。

```sh
bun install          # 根目录：mygo-cli
cd frontend && bun install && cd ..   # 前端依赖
bun run dev          # Go + 前端热更新，启动开发窗口
```

```sh
bun run generate     # 重新生成 frontend/src/mygo.ts（通常 dev/build 会自动做）
go test ./internal/...                # 单元测试
```

## 构建

```sh
bun run build -- -platform windows/amd64,windows/arm64   # exe + NSIS 安装包
bun run build -- -platform darwin/universal              # .app + DMG（需在 macOS 上）
```

无 cgo，可在任意平台交叉编译；macOS 的签名/公证/DMG 需在 macOS 上进行（CI 已配置双平台矩阵）。

## 浏览器插件

`extension/` 是基于 WXT 的 MV3 插件（React + Tailwind，与前端同代技术栈）：背景页用
`chrome.webRequest` 观察响应并按扩展名/Content-Type 分类嗅探，popup 勾选发送，
另提供「用 Knot 下载」右键菜单与 B 站视频页的清晰度卡片。任务通过媒体服务器的
`POST /api/create` 下发（要求 `X-Knot-Client` 请求头防跨站滥用；连接靠绑定地址 +
Host 白名单，与 Web 文件库同一套「无 token」哲学），Knot 需保持运行。

**客户端一键安装**：设置页「浏览器插件」可检测本机浏览器并把打包好的 crx3 直接
安装进去（`internal/service/extinstall.go`）。机制与限制：

- 插件 ID 由提交的 `extension/extension-key.pem` 派生（稳定不变，换钥即换 ID）；
  构建链 `bun run sync:ext` 打包 crx3（`extension/scripts/pack-crx.mjs`，与
  Chromium `crx_creator` 同构）并同步到 `internal/service/extbundle/`（go:embed，
  产物随仓库提交）供应用内安装与 `GET /ext/knot-extension.crx`、
  `GET /ext/updates.xml`（自托管 gupdate 清单）。
- 注册表安装（Windows HKCU，免管理员）：`Software\Google\Chrome\Extensions\<id>`
  （Chromium/Brave 亦读此键）与 `Software\Microsoft\Edge\Extensions\<id>`，
  值 `path`(crx 绝对路径) + `version`；version 变化即重装升级；macOS Edge 走
  用户级 External Extensions JSON。
- **信任放行**：branded Chrome/Edge（Win/Mac）会把非商店外部扩展标记「可能已
  损坏」并禁用（InstallVerifier），用户无法手动启用。需把插件 ID 写入
  `ExtensionInstallAllowlist` 信任策略（设置页「管理员授权」按钮；策略键受限时
  自动弹 UAC 提权）。授权后浏览器可能显示「由您的组织管理」标记。
- Firefox 需 Mozilla 签名无自动通道；应用内只做检测与「打开插件目录」手动加载。

```sh
bun run dev:ext       # 开发模式，自动打开浏览器并装载扩展（热更新）
bun run build:ext     # 构建产物：extension/.output/chrome-mv3
bun run sync:ext      # 构建 + crx3 打包 + 同步 extbundle（插件变更后必须执行并提交）
```

正式使用可在 Knot 设置页「浏览器插件」一键安装；或浏览器扩展页开启开发者模式
后「加载已解压的扩展程序」指向 `extension/.output/chrome-mv3`；打包发布用
`cd extension && bun run zip`。插件设置页可配置 Knot 服务器地址（默认
`http://127.0.0.1:22333`）。

## 结构

```
main.go               应用入口：绑定服务、窗口、生命周期
internal/
  engine/             下载引擎：任务模型、队列管理、持久化
  engine/httpdl/      HTTP 分片下载器（探测/续传/重试/代理）
  engine/hls/         HLS 下载器（播放列表/分片/AES/拼接/转封装 MP4）
  engine/ytdlp/       yt-dlp 封装（二进制管理/-J 解析/画质归并/下载执行）
  service/            mygo 绑定服务（Tasks/App/Browser）与设置
                      内含媒体服务器（Web 文件库 + 插件 API extapi.go）
frontend/             React + Tailwind（Vite+ 工具链）
  src/mygo.ts         自动生成的类型化 IPC 客户端，勿手改
extension/            浏览器插件（WXT + React，MV3）
  lib/                分类/请求/存储工具，口径与后端对齐
  entrypoints/        background / popup / options / B 站内容脚本
```

## License

MIT
