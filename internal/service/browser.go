package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// 浏览模式：主窗口保留应用界面（侧栏可用），站点由无边框子窗口
// 覆盖在主窗口内容区上（Parent 语义保证随主窗口置顶/隐藏/关闭），
// 嗅探浮窗悬浮在站点右上角。切到其他侧栏视图时浏览会话暂停
// （站点窗口隐藏、页面与历史保留），点回"浏览器"原样恢复；只有
// 工具栏"退出"或状态条 × 才真正退出。
const (
	ModeApp      = "app"      // 应用自身界面（无浏览会话）
	ModeBrowsing = "browsing" // 浏览中：站点窗口可见
	ModePaused   = "paused"   // 浏览会话存活但已隐藏（切去了其他视图）
)

// sidebarWidth 与前端侧栏宽度一致（w-44 = 176 CSS px ≈ DIP）。
const sidebarWidth = 176

// browserToolbarH 是浏览模式常驻工具栏的高度，与前端 BrowserToolbar
// 的 h-11（44 CSS px）一致：站点子窗口对齐到工具栏下方。
const browserToolbarH = 44

// BrowserState 是浏览状态（browser-state 事件载荷）。
type BrowserState struct {
	Mode         string `json:"mode"` // app | browsing | paused
	URL          string `json:"url"`  // 站点当前地址
	Title        string `json:"title"`
	CanBack      bool   `json:"canBack"`
	CanForward   bool   `json:"canForward"`
	Loading      bool   `json:"loading"`
	PanelVisible bool   `json:"panelVisible"` // 嗅探浮窗是否可见
}

// Sniffed 是嗅探到的资源条目。
type Sniffed struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"` // hls|video|audio|image|archive|document|link|other
	Page string `json:"page"` // 捕获时所在页面（作 Referer）
}

// BrowserStateEvt 广播浏览状态变化。
var BrowserStateEvt = mygo.NewEvent[BrowserState]("browser-state")

// SniffedEvt 广播嗅探结果（页面加载后自动扫描与手动重新嗅探共用）。
var SniffedEvt = mygo.NewEvent[[]Sniffed]("browser-sniffed")

// DownloadRequest 是网页触发下载时发给前端的确认请求
// （download-request 事件载荷）：前端弹出文件名确认框，用户确认后才
// 调 Browser.Download 入队，取消则什么都不做。
type DownloadRequest struct {
	URL     string `json:"url"`
	Name    string `json:"name"`    // 网页/服务器建议的文件名，可为空
	Referer string `json:"referer"` // 拦截时所在页面（确认时页面可能已切走）
}

// DownloadRequestEvt 广播下载确认请求。
var DownloadRequestEvt = mygo.NewEvent[DownloadRequest]("download-request")

// KnotPreload 注入站点窗口的每个页面：先于页面脚本执行，hook fetch/XHR
// 收集请求 URL，并定义 __knotCollect 供 Go 侧 Eval 拉取。
const KnotPreload = `(function () {
  if (window.__knotInstalled) return;
  window.__knotInstalled = true;
  var seen = new Map();
  var LIMIT = 500;
  var playlists = new Set(); // 出现过的 m3u8 所在目录：其下请求视为分片，不再逐条列出
  var segCount = 0;
  function guess(u) {
    var p = (u || '').split('#')[0].split('?')[0].toLowerCase();
    if (p.slice(-5) === '.m3u8') return 'hls';
    if (/\.(mp4|webm|mkv|mov|m4v|avi|ts|flv)$/.test(p)) return 'video';
    if (/\.(mp3|flac|m4a|aac|wav|ogg|opus)$/.test(p)) return 'audio';
    if (/\.(png|jpe?g|gif|webp|bmp|svg|avif|ico)$/.test(p)) return 'image';
    if (/\.(zip|rar|7z|tar|gz|bz2|xz|apk|exe|dmg|pkg|iso|msi)$/.test(p)) return 'archive';
    if (/\.(pdf|docx?|xlsx?|pptx?|epub|txt|csv)$/.test(p)) return 'document';
    return 'other';
  }
  function dirOf(u) {
    var i = u.lastIndexOf('/');
    return i < 0 ? u : u.slice(0, i + 1);
  }
  function isSegment(u, type) {
    var p = u.split('#')[0].split('?')[0].toLowerCase();
    if (/\.(ts|m4s)$/.test(p)) return true; // 纯 HLS 分片格式
    if (type === 'video' && playlists.size > 0) {
      if (playlists.has(dirOf(u.split('#')[0]))) return true; // 与 m3u8 同目录的媒体请求
    }
    return false;
  }
  // want(t)：Go 侧经 Eval 推送的资源类型白名单（window.__knotTypes），
  // 未推送时全收。被排除的类型不进 seen，不占收集上限。
  function want(t) {
    var w = window.__knotTypes;
    return !w || w.indexOf(t) >= 0;
  }
  function add(raw, forced) {
    try {
      if (!raw || typeof raw !== 'string') return;
      var u = new URL(raw, location.href);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return;
      var h = u.href;
      var g = guess(h);
      if (g === 'hls') playlists.add(dirOf(h.split('#')[0]));
      if (isSegment(h, g)) { segCount++; return; } // 分片聚合到 m3u8 任务，不占列表
      var t = forced || g;
      if (!want(t)) return; // 类型被设置排除：不收集
      if (seen.has(h)) {
        var v = seen.get(h);
        if (forced && v.type === 'other') v.type = forced;
        return;
      }
      if (seen.size >= LIMIT) return;
      seen.set(h, { type: t, page: location.href });
    } catch (e) {}
  }
  function nameOf(u) {
    try {
      var parts = u.split('#')[0].split('?')[0].split('/');
      var n = decodeURIComponent(parts[parts.length - 1] || '');
      return n || u;
    } catch (e) { return u; }
  }
  var of = window.fetch;
  if (of) {
    window.fetch = function (input) {
      try {
        if (typeof input === 'string') add(input);
        else if (input && input.url) add(input.url);
      } catch (e) {}
      return of.apply(this, arguments);
    };
  }
  var xo = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, u) {
    try { add(u); } catch (e) {}
    return xo.apply(this, arguments);
  };
  window.__knotCollect = function () {
    try {
      // 覆盖常规属性与常见懒加载属性（现代站点图片多为 data-src 懒加载）
      var nodes = document.querySelectorAll('img[src],img[data-src],img[data-original],img[data-lazy-src],img[data-lazy],video[src],video source[src],audio[src],audio source[src],a[href],iframe[src],embed[src],object[data]');
      for (var i = 0; i < nodes.length; i++) {
        var el = nodes[i];
        var tag = el.tagName.toLowerCase();
        var u = el.getAttribute('src') || el.getAttribute('data-src') || el.getAttribute('data-original') || el.getAttribute('data-lazy-src') || el.getAttribute('data-lazy') || el.getAttribute('data') || el.getAttribute('href') || '';
        if (!u) continue;
        if (tag === 'a') {
          var g = guess(u);
          add(u, g === 'other' ? 'link' : g);
        } else if (tag === 'img') {
          add(u, 'image');
        } else if (tag === 'video' || tag === 'audio') {
          add(u, tag);
        } else {
          add(u);
        }
      }
    } catch (e) {}
    var out = [];
    seen.forEach(function (v, u) { out.push({ url: u, name: nameOf(u), type: v.type, page: v.page }); });
    out.sort(function (a, b) {
      var order = { hls: 0, video: 1, audio: 2, archive: 3, document: 4, image: 5, link: 6, other: 7 };
      return (order[a.type] === undefined ? 8 : order[a.type]) - (order[b.type] === undefined ? 8 : order[b.type]);
    });
    if (segCount > 0) {
      var hint = playlists.size > 0
        ? '已聚合 ' + segCount + ' 个视频分片（下载对应 HLS 条目即可）'
        : '已聚合 ' + segCount + ' 个视频分片（未发现对应 HLS 列表，可能为分轨流）';
      out.unshift({ url: '', name: hint, type: 'info', page: '' });
    }
    return out;
  };
})();
`

// browserCtl 维护浏览模式、站点子窗口与嗅探浮窗。
// 注意：持有 mu 时不得调用窗口方法（CanGoBack 等会同步等待主线程执行，
// 而主线程事件 handler 也会抢 mu，锁内调用会互锁）。
type browserCtl struct {
	mu           sync.Mutex
	mode         string       // ModeApp | ModeBrowsing | ModePaused
	url          string       // 站点当前地址
	title        string       // 站点页面标题
	loading      bool         // 站点页面是否正在加载
	panelVisible bool         // 嗅探浮窗是否可见（不查窗口，避免阻塞调用）
	panelWanted  bool         // 暂停前浮窗是否可见：恢复时原样还原
	site         *mygo.Window // 覆盖内容区的站点子窗口
	panel        *mygo.Window // 右上角嗅探浮窗
}

var browser = browserCtl{mode: ModeApp}

// Browser 是暴露给前端的浏览器服务（frontend/src/mygo.ts 中的 Browser.*）。
type Browser struct{}

// Open 进入浏览模式：站点子窗口对齐内容区工具栏下方加载 url，嗅探浮窗浮出右上角。
func (b *Browser) Open(url string) error {
	u := normalizeURL(url)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("请输入 http/https 网址")
	}

	// 先置浏览状态，positionSite 依赖它判断。
	browser.mu.Lock()
	browser.mode = ModeBrowsing
	browser.url = u
	browser.title = ""
	browser.loading = false
	browser.mu.Unlock()

	site, err := ensureSite(u)
	if err != nil {
		browser.mu.Lock()
		browser.mode = ModeApp
		browser.url = ""
		browser.mu.Unlock()
		return err
	}
	positionSite()
	site.Show()
	positionSite() // Show 之后再校准一次，避免窗口管理器居中覆盖

	showPanel()
	broadcastBrowserState()
	return nil
}

// Exit 退出浏览模式：隐藏站点窗口与浮窗，回到应用界面。
func (b *Browser) Exit() error {
	exitBrowse()
	return nil
}

// Pause 暂停浏览：隐藏站点窗口与浮窗但会话保留（页面、历史不变），
// 切去其他侧栏视图时调用。Browser.Resume 原样恢复。
func (b *Browser) Pause() error {
	pauseBrowse()
	return nil
}

// Resume 恢复暂停的浏览：站点窗口（与暂停前可见的嗅探浮窗）原样显示。
func (b *Browser) Resume() error {
	resumeBrowse()
	return nil
}

// HidePanel 只隐藏嗅探浮窗（浏览继续）。
func (b *Browser) HidePanel() error {
	browser.mu.Lock()
	p := browser.panel
	browser.mu.Unlock()
	if p != nil {
		p.Hide()
	}
	browser.mu.Lock()
	browser.panelVisible = false
	browser.mu.Unlock()
	broadcastBrowserState()
	return nil
}

// ShowPanel 重新浮出嗅探浮窗：已存在则显示，被销毁则重建，
// 并立即补嗅一次当前页面（面板隐藏期间页面 hook 仍在收集）。
func (b *Browser) ShowPanel() error {
	if !showPanel() {
		return errors.New("未在浏览中")
	}
	go emitSniffed()
	return nil
}

// OpenExternal 用系统默认浏览器打开 url（应对验证码、支付等场景）。
func (b *Browser) OpenExternal(url string) error {
	u := normalizeURL(url)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("请输入 http/https 网址")
	}
	return mygo.Shell.OpenExternal(u)
}

// Navigate 在浏览模式中加载新网址。
func (b *Browser) Navigate(url string) error {
	u := normalizeURL(url)
	if u == "" {
		return errors.New("URL 不能为空")
	}
	browser.mu.Lock()
	site := browser.site
	browser.mu.Unlock()
	if site == nil {
		return b.Open(u)
	}
	return site.Page().LoadURL(u)
}

// Back 后退。
func (b *Browser) Back() error { return withSitePage(func(p *mygo.Page) { p.GoBack() }) }

// Forward 前进。
func (b *Browser) Forward() error { return withSitePage(func(p *mygo.Page) { p.GoForward() }) }

// Reload 重新加载当前页。
func (b *Browser) Reload() error { return withSitePage(func(p *mygo.Page) { p.Reload() }) }

// Stop 停止当前加载。
func (b *Browser) Stop() error { return withSitePage(func(p *mygo.Page) { p.Stop() }) }

// State 返回当前浏览状态。
func (b *Browser) State() (BrowserState, error) { return browserSnapshot(), nil }

// Scan 立即嗅探当前页面资源并广播结果。
func (b *Browser) Scan() error {
	go emitSniffed()
	return nil
}

// Download 把嗅探到的资源加入下载队列；referer 为捕获时所在页面，
// 并自动携带站点 Cookie 与浏览器 UA 规避防盗链。
func (b *Browser) Download(url, name, referer string) error {
	go func() {
		opt := CreateOptions{Name: name, Referer: referer}
		opt.UserAgent = siteUserAgent()
		opt.Cookie = cookiesFor(normalizeURL(url))
		if _, err := createTask(context.Background(), normalizeURL(url), opt); err != nil {
			Fatal("添加下载失败", err)
		}
	}()
	return nil
}

// --- 站点子窗口 ---

// ensureSite 返回站点子窗口：首次创建（含页面钩子），已存在则导航。
func ensureSite(u string) (*mygo.Window, error) {
	browser.mu.Lock()
	site := browser.site
	browser.mu.Unlock()

	if site != nil {
		return site, site.Page().LoadURL(u)
	}

	mainMu.Lock()
	main := mainWin
	mainMu.Unlock()
	if main == nil {
		return nil, ErrNotReady
	}

	w := mygo.NewWindow(mygo.WindowOptions{
		URL:           u,
		Parent:        main, // 随主窗口置顶/隐藏，主窗口关闭时一并关闭
		Frameless:     true, // 无边框，融入内容区
		DisableShadow: true, // 无阴影，视觉上属于页面的一部分
		SkipTaskbar:   true,
		Hidden:        true,
		MinWidth:      320,
		MinHeight:     240,
		Page: mygo.PageOptions{
			PreloadScript: KnotPreload,
			Untrusted:     true, // 外部站点：禁其调用 Go 服务、禁其默认下载
		},
	})
	attachSiteHooks(w)
	w.OnClosed(func() {
		browser.mu.Lock()
		browser.site = nil
		browser.mu.Unlock()
	})

	browser.mu.Lock()
	browser.site = w
	browser.mu.Unlock()
	return w, nil
}

// attachSiteHooks 给站点页面注册导航/弹窗/加载/嗅探/下载拦截钩子。
func attachSiteHooks(win *mygo.Window) {
	page := win.Page()
	page.OnWillNavigate(func(e *mygo.NavigateEvent) {
		// LoadURL 不经过此回调；这里约束页面自发导航。
		if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
			e.PreventDefault()
		}
	})
	page.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		// target=_blank / window.open：用户点击的 http(s) 链接在站点窗口
		// 内原地打开（返回 nil 即不另开窗口）；脚本自弹的窗一律拒绝。
		if req.UserInitiated {
			u := normalizeURL(req.URL)
			if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				_ = page.LoadURL(u)
			}
		}
		return nil
	})
	page.OnDidNavigate(func(url string) {
		setLoading(true)
		browser.mu.Lock()
		browser.url = url
		browser.mu.Unlock()
		broadcastBrowserState()
		time.AfterFunc(2*time.Second, emitSniffed) // 页面就绪后自动嗅探
	})
	page.OnDOMReady(func() {
		// 每次导航都是新的 JS 上下文，嗅探类型白名单要重新推送。
		go pushSniffTypes()
	})
	page.OnDidFinishLoad(func() { setLoading(false) })
	page.OnDidFailLoad(func(*mygo.LoadError) { setLoading(false) })
	page.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
		browser.mu.Lock()
		browser.title = e.Title
		browser.mu.Unlock()
		broadcastBrowserState()
	})
	page.OnWillDownload(func(e *mygo.DownloadEvent) {
		// 不走 webview 自带下载：弹出嗅探浮窗，让用户确认文件名后再入队。
		e.PreventDefault()
		if e.URL == "" {
			return
		}
		req := DownloadRequest{URL: e.URL, Name: e.SuggestedName}
		browser.mu.Lock()
		req.Referer = browser.url
		noSession := browser.mode == ModeApp
		browser.mu.Unlock()
		if noSession {
			return
		}
		go func() {
			showPanel() // 浮窗被收起时自动浮出，承载确认框
			_ = DownloadRequestEvt.Broadcast(req)
		}()
	})
}

// positionSite 把站点子窗口对齐到主窗口内容区（扣除侧栏与顶部工具栏）。
func positionSite() {
	browser.mu.Lock()
	site := browser.site
	browsing := browser.mode == ModeBrowsing
	browser.mu.Unlock()
	if site == nil || !browsing {
		return
	}
	mainMu.Lock()
	main := mainWin
	mainMu.Unlock()
	if main == nil {
		return
	}
	cb := main.ContentBounds()
	if cb.Width <= sidebarWidth || cb.Height <= browserToolbarH {
		return
	}
	site.SetBounds(mygo.Rectangle{
		X:      cb.X + sidebarWidth,
		Y:      cb.Y + browserToolbarH,
		Width:  cb.Width - sidebarWidth,
		Height: cb.Height - browserToolbarH,
	})
}

func withSitePage(fn func(p *mygo.Page)) error {
	browser.mu.Lock()
	site := browser.site
	browser.mu.Unlock()
	if site == nil {
		return errors.New("浏览器未打开")
	}
	fn(site.Page())
	return nil
}

// siteUserAgent 返回站点窗口的 UA；未打开时返回空串。
func siteUserAgent() string {
	browser.mu.Lock()
	site := browser.site
	browser.mu.Unlock()
	if site == nil {
		return ""
	}
	return site.Page().UserAgent()
}

// cookiesFor 取默认会话中请求 targetURL 会携带的 Cookie 串。
func cookiesFor(targetURL string) string {
	if targetURL == "" {
		return ""
	}
	cookies, err := mygo.Cookies.List(mygo.CookieFilter{URL: targetURL})
	if err != nil || len(cookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// resetBrowse 复位浏览状态（主窗口被重建时调用）。
func resetBrowse() {
	browser.mu.Lock()
	wasBrowsing := browser.mode != ModeApp
	site := browser.site
	panel := browser.panel
	browser.mode = ModeApp
	browser.url = ""
	browser.title = ""
	browser.loading = false
	browser.panelVisible = false
	browser.panelWanted = false
	browser.mu.Unlock()
	if site != nil {
		site.Hide()
	}
	if panel != nil {
		panel.Hide()
	}
	if wasBrowsing {
		broadcastBrowserState()
	}
}

func exitBrowse() {
	browser.mu.Lock()
	browser.mode = ModeApp
	browser.url = ""
	browser.title = ""
	browser.loading = false
	browser.panelVisible = false
	browser.panelWanted = false
	site := browser.site
	panel := browser.panel
	browser.mu.Unlock()
	if site != nil {
		site.Hide()
	}
	if panel != nil {
		panel.Hide()
	}
	broadcastBrowserState()
}

// pauseBrowse 暂停浏览会话：窗口藏起、声音静掉，页面与历史原地保留。
func pauseBrowse() {
	browser.mu.Lock()
	if browser.mode != ModeBrowsing {
		browser.mu.Unlock()
		return
	}
	browser.mode = ModePaused
	browser.panelWanted = browser.panelVisible
	browser.panelVisible = false
	site := browser.site
	panel := browser.panel
	browser.mu.Unlock()
	if site != nil {
		site.Hide()
		site.Page().SetAudioMuted(true) // 隐藏的页面不该继续出声
	}
	if panel != nil {
		panel.Hide()
	}
	broadcastBrowserState()
}

// resumeBrowse 恢复暂停的浏览会话：窗口原样显示，暂停前可见的浮窗跟随浮出。
func resumeBrowse() {
	browser.mu.Lock()
	if browser.mode != ModePaused || browser.site == nil {
		browser.mu.Unlock()
		return
	}
	browser.mode = ModeBrowsing
	site := browser.site
	wanted := browser.panelWanted
	browser.mu.Unlock()
	positionSite()
	site.Show()
	positionSite() // Show 之后再校准一次
	site.Page().SetAudioMuted(false)
	if wanted {
		showPanel() // 内部会广播状态
	} else {
		broadcastBrowserState()
	}
}

// setLoading 更新加载状态并广播（由主线程页面钩子调用）。
func setLoading(v bool) {
	browser.mu.Lock()
	if browser.loading == v {
		browser.mu.Unlock()
		return
	}
	browser.loading = v
	browser.mu.Unlock()
	broadcastBrowserState()
}

func browserSnapshot() BrowserState {
	// 锁内只读标量字段；CanGoBack 等会同步等主线程，必须在锁外调用，
	// 否则与主线程事件 handler（broadcastBrowserState 抢锁）互锁。
	browser.mu.Lock()
	st := BrowserState{
		Mode:         browser.mode,
		URL:          browser.url,
		Title:        browser.title,
		Loading:      browser.loading,
		PanelVisible: browser.panelVisible,
	}
	site := browser.site
	browsing := browser.mode == ModeBrowsing
	browser.mu.Unlock()
	if browsing && site != nil {
		p := site.Page()
		st.CanBack = p.CanGoBack()
		st.CanForward = p.CanGoForward()
	}
	return st
}

func broadcastBrowserState() {
	_ = BrowserStateEvt.Broadcast(browserSnapshot())
}

// emitSniffed 从站点页面拉取嗅探结果并广播（仅浏览模式且浮窗可见时；
// 页面 preload hook 始终在收集，浮窗重开时会补拉一次）。
func emitSniffed() {
	browser.mu.Lock()
	browsing := browser.mode == ModeBrowsing
	visible := browser.panelVisible
	site := browser.site
	pageURL := browser.url
	browser.mu.Unlock()
	if !browsing || !visible || site == nil {
		return
	}
	list, err := mygo.EvalAs[[]Sniffed](site.Page(), "window.__knotCollect ? window.__knotCollect() : []")
	if err != nil || list == nil {
		list = []Sniffed{}
	}

	// 哔哩哔哩视频页：页面走 DASH 分轨（m4s 分片聚合嗅探不到整段地址），
	// 追加专用下载条目排在最前，浮窗据此展示画质选择。biliFetch 内含
	// playinfo 空壳时的 playurl 接口兜底；拿不到流就不出卡片。
	if isBilibiliVideoPage(pageURL) {
		if p, page, err := biliFetch(); err == nil && p.hasStreams() {
			list = append([]Sniffed{{URL: page, Name: biliDefaultName(p), Type: "bili", Page: page}}, list...)
		}
	} else if isYouTubeVideoPage(pageURL) {
		// YouTube 视频页：解析与下载走 yt-dlp，无需页面上下文，条目只是
		// 把链接送进专用弹窗（标题取页面标题，去掉站点后缀）。
		const titleCode = `document.title.replace(/\s*-\s*YouTube\s*$/, '')`
		if title, err := mygo.EvalAs[string](site.Page(), titleCode); err == nil {
			if title = strings.TrimSpace(title); title != "" {
				list = append([]Sniffed{{URL: pageURL, Name: title, Type: "youtube", Page: pageURL}}, list...)
			}
		}
	}
	list = filterSniffed(list)
	_ = SniffedEvt.Broadcast(list)
}

// filterSniffed 按设置的资源类型白名单过滤嗅探结果。收集层（preload 的
// want）已排除大部分，这里兜底导航早期白名单推送前的收集，并让重新
// 勾选后历史条目立即恢复。info（分片聚合提示）只为 HLS 服务，hls 被
// 排除时不显示。
func filterSniffed(list []Sniffed) []Sniffed {
	if settings == nil {
		return list
	}
	allowed := sniffTypeSet(settings.Get())
	if len(allowed) == 0 || len(allowed) == len(AllSniffTypes) {
		return list
	}
	out := make([]Sniffed, 0, len(list))
	for _, s := range list {
		switch {
		case s.Type == "info":
			if allowed["hls"] {
				out = append(out, s)
			}
		case s.Type == "bili":
			// B 站专用条目是视频资源：video 或 hls 任一允许即展示。
			if allowed["video"] || allowed["hls"] {
				out = append(out, s)
			}
		case s.Type == "youtube":
			// YouTube 专用条目同 B 站：视频资源白名单放行。
			if allowed["video"] || allowed["hls"] {
				out = append(out, s)
			}
		case allowed[s.Type]:
			out = append(out, s)
		}
	}
	return out
}

// pushSniffTypes 把嗅探类型白名单推给站点页面（window.__knotTypes，
// preload 的 want 据此在收集时就排除，不占 500 条收集上限）。
// 由 DOMReady 与设置变更触发；未推送时 preload 全收，由输出层过滤兜底。
func pushSniffTypes() {
	if settings == nil {
		return
	}
	browser.mu.Lock()
	site := browser.site
	browser.mu.Unlock()
	if site == nil {
		return
	}
	var code string
	if types := settings.Get().SniffTypes; len(types) == 0 {
		code = "window.__knotTypes = null" // 空 = 全部（sanitize 后不应出现，防御）
	} else {
		data, err := json.Marshal(types)
		if err != nil {
			return
		}
		code = "window.__knotTypes = " + string(data)
	}
	_, _ = site.Page().Eval(code)
}

// UpdateSniffConfig 在嗅探类型设置变化后调用：立即推送白名单并重新
// 嗅探，浮窗即时生效（无需刷新页面或重开浏览）。
func UpdateSniffConfig() {
	pushSniffTypes()
	go emitSniffed()
}

// --- 嗅探浮窗 ---

const panelWidth, panelHeight = 380, 560

// showPanel 浮出嗅探浮窗（已存在则显示，被销毁则重建）。
// 返回是否有浏览会话（暂停态也允许：网页下载确认框需要浮窗承载）。
func showPanel() bool {
	browser.mu.Lock()
	hasSession := browser.mode != ModeApp
	p := browser.panel
	browser.mu.Unlock()
	if !hasSession {
		return false
	}
	if p != nil {
		p.Show()
	} else {
		mainMu.Lock()
		main := mainWin
		mainMu.Unlock()

		w := mygo.NewWindow(mygo.WindowOptions{
			URL:             "/#panel",
			Width:           panelWidth,
			Height:          panelHeight,
			Parent:          main, // 随主窗口置顶，最小化时隐藏
			Frameless:       true, // 无边框：页内自带标题栏（可拖动、收起）
			AlwaysOnTop:     true,
			SkipTaskbar:     true,
			DisableResize:   true,
			BackgroundColor: "light-dark(#ffffff, #1d1d20)",
		})
		w.OnClosed(func() {
			browser.mu.Lock()
			browser.panel = nil
			browser.panelVisible = false
			browser.mu.Unlock()
			broadcastBrowserState()
		})
		browser.mu.Lock()
		browser.panel = w
		browser.mu.Unlock()
		w.Show()
	}
	positionPanel()
	browser.mu.Lock()
	browser.panelVisible = true
	browser.mu.Unlock()
	broadcastBrowserState()
	return true
}

// positionPanel 把浮窗贴到主窗口内容区右上角（工具栏下方）。
func positionPanel() {
	browser.mu.Lock()
	panel := browser.panel
	browser.mu.Unlock()
	if panel == nil {
		return
	}
	mainMu.Lock()
	main := mainWin
	mainMu.Unlock()
	if main == nil {
		return
	}
	cb := main.ContentBounds()
	panel.SetPosition(cb.X+cb.Width-panelWidth-12, cb.Y+browserToolbarH+8)
}

// sniffLoop 周期性重扫当前站点页面：懒加载/滚动加载的内容在浏览过程中
// 持续进入嗅探列表（非浏览模式时空转）。
func sniffLoop() {
	t := time.NewTicker(8 * time.Second)
	defer t.Stop()
	for range t.C {
		emitSniffed()
	}
}

// normalizeURL 补全省略的 scheme；空串原样返回。
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if i := strings.Index(raw, "://"); i > 0 {
		return raw // 其他 scheme 交给导航拦截处理
	}
	return "https://" + raw
}
