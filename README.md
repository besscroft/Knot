# Knot

基于 [MyGo](https://mygo.egoist.dev/) 的跨平台资源下载器（Windows / macOS），Go 后端 + React 前端（Vite+ 工具链），系统 WebView 渲染，无 cgo。

## 功能

- HTTP/HTTPS 直链下载：多线程分片（默认 8 段）、断点续传、失败自动重试
- **HLS (m3u8) 下载**：master 自动选最高清晰度、分片并发下载、AES-128 解密、断点续传，完成后自动合并为 MP4（gomedia 纯 Go 转封装，失败时保留 .ts）
- **格式选择与转换**（参考 Downbit）：下载时「保存为」可选 MP4/MKV/MOV/WebM 或提取音频为 M4A/MP3/Opus/FLAC/WAV；容器转换与音频提取均为 FFmpeg 流拷贝，速度极快；FFmpeg 缺失时自动下载对应平台构建并校验（仅首次），也可在设置中手动指定路径；设置里可配置视频/音频默认格式
- 下载队列：并发上限可配、暂停/继续/删除、任务栏进度
- **应用内浏览 + 资源嗅探**：浏览时侧栏保留，站点由无边框子窗口呈现在内容区，右上角浮出嗅探面板（自动收集 fetch/XHR 与 DOM 资源，勾选下载）；下载自动携带站点的 Referer/Cookie/UA 规避防盗链；网页触发的下载自动转入下载器；侧栏「浏览中」状态条或嗅探面板均可退出浏览
- **下载代理**：支持 http/https/socks5，留空跟随系统/环境代理（仅作用于下载器；内置浏览始终走系统代理——webview 代理由 mygo/平台决定）
- 应用设置：下载目录、分片数、并发数、代理
- 类型安全 IPC：Go 服务方法自动生成 TypeScript 客户端（`frontend/src/mygo.ts`）
- 深浅色跟随系统；单实例锁；Windows 关窗退出 / macOS 关窗驻留

### 已知限制

- 资源嗅探只覆盖顶层 frame（iframe 内资源暂不采集）
- HLS 暂不支持：直播流（无 EXT-X-ENDLIST）、fMP4 分片（EXT-X-MAP）、BYTERANGE、SAMPLE-AES
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

## 结构

```
main.go               应用入口：绑定服务、窗口、生命周期
internal/
  engine/             下载引擎：任务模型、队列管理、持久化
  engine/httpdl/      HTTP 分片下载器（探测/续传/重试/代理）
  engine/hls/         HLS 下载器（播放列表/分片/AES/拼接/转封装 MP4）
  service/            mygo 绑定服务（Tasks/App/Browser）与设置
frontend/             React + Tailwind（Vite+ 工具链）
  src/mygo.ts         自动生成的类型化 IPC 客户端，勿手改
```

## License

MIT
