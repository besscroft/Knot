package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// 浏览模式：主窗口保留应用界面（侧栏可用），站点由无边框子窗口
// 覆盖在主窗口内容区上（Parent 语义保证随主窗口置顶/隐藏/关闭），
// 嗅探浮窗悬浮在站点右上角。退出浏览即回到应用界面。
const (
	ModeApp      = "app"      // 应用自身界面
	ModeBrowsing = "browsing" // 正在浏览外部网站
)

// sidebarWidth 与前端侧栏宽度一致（w-44 = 176 CSS px ≈ DIP）。
const sidebarWidth = 176

// BrowserState 是浏览状态（browser-state 事件载荷）。
type BrowserState struct {
	Mode       string `json:"mode"` // app | browsing
	URL        string `json:"url"`  // 站点当前地址
	Title      string `json:"title"`
	CanBack    bool   `json:"canBack"`
	CanForward bool   `json:"canForward"`
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
  function add(raw, forced) {
    try {
      if (!raw || typeof raw !== 'string') return;
      var u = new URL(raw, location.href);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return;
      var h = u.href;
      var g = guess(h);
      if (g === 'hls') playlists.add(dirOf(h.split('#')[0]));
      if (isSegment(h, g)) { segCount++; return; } // 分片聚合到 m3u8 任务，不占列表
      if (seen.has(h)) {
        var v = seen.get(h);
        if (forced && v.type === 'other') v.type = forced;
        return;
      }
      if (seen.size >= LIMIT) return;
      seen.set(h, { type: forced || g, page: location.href });
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
      out.unshift({ url: '', name: '已聚合 ' + segCount + ' 个视频分片（下载对应 HLS 条目即可）', type: 'info', page: '' });
    }
    return out;
  };
})();
`

// browserCtl 维护浏览模式、站点子窗口与嗅探浮窗。
type browserCtl struct {
	mu    sync.Mutex
	mode  string       // ModeApp | ModeBrowsing
	url   string       // 站点当前地址
	title string       // 站点页面标题
	site  *mygo.Window // 覆盖内容区的站点子窗口
	panel *mygo.Window // 右上角嗅探浮窗
}

var browser = browserCtl{mode: ModeApp}

// Browser 是暴露给前端的浏览器服务（frontend/src/mygo.ts 中的 Browser.*）。
type Browser struct{}

// Open 进入浏览模式：站点子窗口覆盖内容区加载 url，嗅探浮窗浮出右上角。
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
	content, sb := siteBoundsDebug()
	log.Printf("browse: content=%+v site=%+v", content, sb)

	ensurePanel()
	positionPanel()
	broadcastBrowserState()
	return nil
}

// siteBoundsDebug 返回主窗口内容区与站点窗口的当前坐标（诊断用）。
func siteBoundsDebug() (content, site mygo.Rectangle) {
	mainMu.Lock()
	main := mainWin
	mainMu.Unlock()
	if main != nil {
		content = main.ContentBounds()
	}
	browser.mu.Lock()
	s := browser.site
	browser.mu.Unlock()
	if s != nil {
		site = s.Bounds()
	}
	return content, site
}

// Exit 退出浏览模式：隐藏站点窗口与浮窗，回到应用界面。
func (b *Browser) Exit() error {
	exitBrowse()
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
	return nil
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

// attachSiteHooks 给站点页面注册导航/嗅探/下载拦截钩子。
func attachSiteHooks(win *mygo.Window) {
	page := win.Page()
	page.OnWillNavigate(func(e *mygo.NavigateEvent) {
		// LoadURL 不经过此回调；这里约束页面自发导航。
		if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
			e.PreventDefault()
		}
	})
	page.OnDidNavigate(func(url string) {
		browser.mu.Lock()
		browser.url = url
		browser.mu.Unlock()
		broadcastBrowserState()
		time.AfterFunc(2*time.Second, emitSniffed) // 页面就绪后自动嗅探
	})
	page.OnPageTitleUpdated(func(e *mygo.TitleEvent) {
		browser.mu.Lock()
		browser.title = e.Title
		browser.mu.Unlock()
		broadcastBrowserState()
	})
	page.OnWillDownload(func(e *mygo.DownloadEvent) {
		// 不走 webview 自带下载，转给引擎并携带浏览上下文。
		e.PreventDefault()
		if e.URL == "" {
			return
		}
		go func() {
			opt := CreateOptions{Name: e.SuggestedName}
			browser.mu.Lock()
			opt.Referer = browser.url
			browser.mu.Unlock()
			opt.UserAgent = siteUserAgent()
			opt.Cookie = cookiesFor(e.URL)
			if _, err := createTask(context.Background(), e.URL, opt); err != nil {
				Fatal("添加下载失败", err)
			}
		}()
	})
}

// positionSite 把站点子窗口对齐到主窗口内容区（扣除侧栏）。
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
	if cb.Width <= sidebarWidth {
		return
	}
	site.SetBounds(mygo.Rectangle{
		X:      cb.X + sidebarWidth,
		Y:      cb.Y,
		Width:  cb.Width - sidebarWidth,
		Height: cb.Height,
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
	wasBrowsing := browser.mode == ModeBrowsing
	site := browser.site
	panel := browser.panel
	browser.mode = ModeApp
	browser.url = ""
	browser.title = ""
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

func browserSnapshot() BrowserState {
	browser.mu.Lock()
	defer browser.mu.Unlock()
	st := BrowserState{Mode: browser.mode, URL: browser.url, Title: browser.title}
	if browser.mode == ModeBrowsing && browser.site != nil {
		p := browser.site.Page()
		st.CanBack = p.CanGoBack()
		st.CanForward = p.CanGoForward()
	}
	return st
}

func broadcastBrowserState() {
	_ = BrowserStateEvt.Broadcast(browserSnapshot())
}

// emitSniffed 从站点页面拉取嗅探结果并广播（仅浏览模式）。
func emitSniffed() {
	browser.mu.Lock()
	browsing := browser.mode == ModeBrowsing
	site := browser.site
	browser.mu.Unlock()
	if !browsing || site == nil {
		return
	}
	list, err := mygo.EvalAs[[]Sniffed](site.Page(), "window.__knotCollect ? window.__knotCollect() : []")
	if err != nil || list == nil {
		list = []Sniffed{}
	}
	_ = SniffedEvt.Broadcast(list)
}

// --- 嗅探浮窗 ---

const panelWidth, panelHeight = 380, 560

// ensurePanel 懒创建嗅探浮窗（已存在则显示）。
func ensurePanel() {
	browser.mu.Lock()
	p := browser.panel
	browser.mu.Unlock()
	if p != nil {
		p.Show()
		return
	}
	mainMu.Lock()
	main := mainWin
	mainMu.Unlock()

	w := mygo.NewWindow(mygo.WindowOptions{
		Title:           "资源嗅探",
		URL:             "/#panel",
		Width:           panelWidth,
		Height:          panelHeight,
		Parent:          main, // 随主窗口置顶，最小化时隐藏
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableResize:   true,
		BackgroundColor: "light-dark(#ffffff, #1d1d20)",
	})
	w.OnClosed(func() {
		browser.mu.Lock()
		browser.panel = nil
		browser.mu.Unlock()
	})
	browser.mu.Lock()
	browser.panel = w
	browser.mu.Unlock()
	w.Show()
}

// positionPanel 把浮窗贴到主窗口内容区右上角。
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
	panel.SetPosition(cb.X+cb.Width-panelWidth-12, cb.Y+8)
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
