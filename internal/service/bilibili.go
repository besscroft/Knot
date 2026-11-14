// 哔哩哔哩视频页支持：B 站走 DASH 分轨（视频/音频独立直链，CDN 校验
// Referer），嗅探只看得到 m4s 分片拿不到整段地址。这里从站点页面拉取
// window.__playinfo__（DASH 清单）与 window.__INITIAL_STATE__（标题/分 P），
// 在嗅探浮窗给出画质选择；下载登记为 engine 的 dash 双流任务（分别下载
// 后 FFmpeg 合流）。画质随登录态（页面 Cookie）决定，与网页播放器一致。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/besscroft/knot/internal/engine"
	"github.com/besscroft/knot/internal/engine/httpdl"
	"github.com/egoist/mygo"
)

// BilibiliInfo 是 B 站视频页的解析结果（Browser.BilibiliInfo 返回值）。
type BilibiliInfo struct {
	Title     string        `json:"title"`
	Page      int           `json:"page"`  // 当前分 P（1 起）
	Pages     int           `json:"pages"` // 总 P 数
	Dash      bool          `json:"dash"`  // true = DASH 分轨；false = 旧版 durl 单文件
	Qualities []BiliQuality `json:"qualities"`
	Audio     string        `json:"audio"` // 将采用的音轨描述（DASH 模式）
}

// BiliQuality 是一个可选画质；大小按码率×时长估算。
type BiliQuality struct {
	ID     int    `json:"id"`   // playinfo 画质号；durl 模式为 0
	Desc   string `json:"desc"` // 如 "高清 1080P60"
	Codecs string `json:"codecs,omitempty"`
	Size   int64  `json:"size"` // 估算字节数，0 = 未知
}

// biliStreamPayload 是播放信息里的一路流（视频或音频）。
type biliStreamPayload struct {
	ID        int    `json:"id"`
	Codecs    string `json:"codecs"`
	Bandwidth int64  `json:"bandwidth"` // bit/s
	Base      string `json:"base"`
}

// biliPayload 是 biliEvalCode 的返回结构（站点页面 Eval 拉取），也是
// playurl 接口兜底的最终形态。
type biliPayload struct {
	OK         bool                `json:"ok"`
	Dash       bool                `json:"dash"`
	TimeLength int64               `json:"timelength"` // 毫秒，估算大小用
	Title      string              `json:"title"`
	Page       int                 `json:"page"`
	Pages      int                 `json:"pages"`
	Part       string              `json:"part"`
	CID        int64               `json:"cid"`
	BVID       string              `json:"bvid"`
	AID        int64               `json:"aid"`
	AcceptIDs  []int               `json:"acceptIds"`
	AcceptDesc []string            `json:"acceptDesc"`
	Videos     []biliStreamPayload `json:"videos"`
	Audios     []biliStreamPayload `json:"audios"`
	Durl       []biliDurlItem      `json:"durl"`
}

// biliDurlItem 是旧版 durl 模式的单文件条目。
type biliDurlItem struct {
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// hasStreams 报告播放信息里是否真的有可下载的流：B 站部分页面嵌入的
// __playinfo__ 只是空壳 {}，由播放器异步填充，需要接口兜底。
func (p *biliPayload) hasStreams() bool {
	if p.Dash {
		return len(p.Videos) > 0 || len(p.Audios) > 0
	}
	return len(p.Durl) > 0
}

// biliEvalCode 在视频页面内求值：取播放清单与标题/分 P 元数据。
// playinfo 由服务端渲染内嵌，切分 P 时整页刷新，始终对应当前页面。
const biliEvalCode = `(function () {
  var pi = window.__playinfo__;
  var st = window.__INITIAL_STATE__;
  if (!pi) return { ok: false };
  function streams(list) {
    return (list || []).map(function (s) {
      return { id: s.id, codecs: s.codecs || '', bandwidth: s.bandwidth || 0,
               base: s.baseUrl || s.base_url || '' };
    });
  }
  var pages = (st && st.videoData && st.videoData.pages) || [];
  var q = new URLSearchParams(location.search);
  var p = parseInt(q.get('p') || '1', 10) || 1;
  var cur = pages[p - 1] || {};
  var title = (st && st.videoData && st.videoData.title) || '';
  if (!title) title = document.title.replace(/_哔哩哔哩.*$/, '');
  return {
    ok: true,
    dash: !!pi.dash,
    timelength: pi.timelength || 0,
    title: title,
    page: p,
    pages: pages.length || 1,
    part: cur.part || '',
    cid: cur.cid || (st && st.videoData && st.videoData.cid) || 0,
    bvid: (st && st.videoData && st.videoData.bvid) || '',
    aid: (st && st.videoData && st.videoData.aid) || 0,
    acceptIds: pi.accept_quality || [],
    acceptDesc: pi.accept_description || [],
    videos: pi.dash ? streams(pi.dash.video) : [],
    audios: pi.dash ? streams(pi.dash.audio) : [],
    durl: (pi.durl || []).map(function (d) { return { url: d.url || '', size: d.size || 0 }; })
  };
})()`

// isBilibiliVideoPage 报告 URL 是否为 B 站视频页（www/m 主域 + /video/ 路径）。
func isBilibiliVideoPage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.bilibili.com", "m.bilibili.com", "bilibili.com":
	default:
		return false
	}
	return strings.HasPrefix(u.Path, "/video/")
}

// biliFetch 从当前站点页面拉取 B 站播放信息，返回 payload 与页面地址。
// BilibiliInfo 与 BilibiliDownload 共用，保证弹窗看到的与实际下载的一致。
// playinfo 是空壳（没有可用流）时回退到 playurl 接口。
func biliFetch() (*biliPayload, string, error) {
	browser.mu.Lock()
	site := browser.site
	pageURL := browser.url
	browser.mu.Unlock()
	if site == nil {
		return nil, "", errors.New("未在浏览中")
	}
	if !isBilibiliVideoPage(pageURL) {
		return nil, "", errors.New("当前页面不是哔哩哔哩视频页")
	}
	payload, err := mygo.EvalAs[biliPayload](site.Page(), biliEvalCode)
	if err != nil {
		return nil, "", fmt.Errorf("读取页面播放信息失败: %w", err)
	}
	if !payload.OK {
		return nil, "", errors.New("页面尚未加载播放信息，请稍后重试或刷新页面")
	}
	if !payload.hasStreams() {
		if api, apiErr := biliFetchAPI(&payload, pageURL); apiErr == nil {
			// 标题/分 P 元数据仍以页面为准（接口不含这些字段）。
			api.Title = payload.Title
			api.Page = payload.Page
			api.Pages = payload.Pages
			api.Part = payload.Part
			payload = *api
		}
	}
	if !payload.hasStreams() {
		return nil, "", errors.New("未能获取到视频流（可能需要登录或该视频受限）")
	}
	return &payload, pageURL, nil
}

// biliAPIData 是 playurl 接口（x/player/playurl，fnval=4048）的 data 字段，
// 流结构在 Go 侧转换（接口用 snake_case 的 base_url 等）。
type biliAPIData struct {
	Dash *struct {
		Video []biliAPIStream `json:"video"`
		Audio []biliAPIStream `json:"audio"`
	} `json:"dash"`
	Durl              []biliDurlItem `json:"durl"`
	AcceptQuality     []int          `json:"accept_quality"`
	AcceptDescription []string       `json:"accept_description"`
	TimeLength        int64          `json:"timelength"`
}

// biliAPIStream 是接口返回的一路流（视频或音频）。
type biliAPIStream struct {
	ID        int64  `json:"id"`
	Codecs    string `json:"codecs"`
	Bandwidth int64  `json:"bandwidth"`
	BaseURL   string `json:"base_url"`
}

// biliPayloadFromAPI 把接口 data 转换为统一的播放信息。
func biliPayloadFromAPI(data *biliAPIData) *biliPayload {
	p := &biliPayload{
		OK:         true,
		Dash:       data.Dash != nil,
		TimeLength: data.TimeLength,
		AcceptIDs:  data.AcceptQuality,
		AcceptDesc: data.AcceptDescription,
		Durl:       data.Durl,
	}
	if data.Dash != nil {
		for _, v := range data.Dash.Video {
			if v.BaseURL == "" {
				continue
			}
			p.Videos = append(p.Videos, biliStreamPayload{
				ID: int(v.ID), Codecs: v.Codecs, Bandwidth: v.Bandwidth, Base: v.BaseURL,
			})
		}
		for _, a := range data.Dash.Audio {
			if a.BaseURL == "" {
				continue
			}
			p.Audios = append(p.Audios, biliStreamPayload{
				ID: int(a.ID), Codecs: a.Codecs, Bandwidth: a.Bandwidth, Base: a.BaseURL,
			})
		}
	}
	return p
}

// biliVideoIDRe 匹配视频页 URL 里的 BV 号（固定 BV+10 位）或 av 号。
var biliVideoIDRe = regexp.MustCompile(`(?i)/video/((?:BV[0-9A-Za-z]{10})|av\d+)`)

// biliVideoIDFromURL 从页面 URL 提取视频 ID（BVxxx / av123），无则空串。
func biliVideoIDFromURL(raw string) string {
	m := biliVideoIDRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// biliFetchAPI 调用 playurl 接口获取播放清单（playinfo 空壳时兜底）。
// Cookie 随请求携带（画质随登录态），普通视频接口无需 WBI 签名。
func biliFetchAPI(meta *biliPayload, pageURL string) (*biliPayload, error) {
	q := url.Values{}
	switch {
	case meta.BVID != "":
		q.Set("bvid", meta.BVID)
	case meta.AID > 0:
		q.Set("aid", strconv.FormatInt(meta.AID, 10))
	default:
		id := biliVideoIDFromURL(pageURL)
		if id == "" {
			return nil, errors.New("无法确定视频 ID")
		}
		if aid, ok := strings.CutPrefix(id, "av"); ok {
			q.Set("aid", aid)
		} else {
			q.Set("bvid", id)
		}
	}
	if meta.CID > 0 {
		q.Set("cid", strconv.FormatInt(meta.CID, 10))
	}
	q.Set("fnval", "4048") // DASH + HDR/杜比/8K 全格式
	q.Set("fnver", "0")
	q.Set("fourk", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.bilibili.com/x/player/playurl?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://www.bilibili.com/")
	if ua := siteUserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if ck := cookiesFor(pageURL); ck != "" {
		req.Header.Set("Cookie", ck)
	}
	resp, err := httpdl.Client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 playurl 接口失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playurl 接口返回 %s", resp.Status)
	}
	var out struct {
		Code    int         `json:"code"`
		Message string      `json:"message"`
		Data    biliAPIData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析 playurl 接口响应失败: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("playurl 接口返回 %d: %s", out.Code, out.Message)
	}
	return biliPayloadFromAPI(&out.Data), nil
}

// BilibiliInfo 返回当前 B 站视频页可下载的画质信息（嗅探卡片弹窗用）。
func (b *Browser) BilibiliInfo() (BilibiliInfo, error) {
	payload, _, err := biliFetch()
	if err != nil {
		return BilibiliInfo{}, err
	}
	return biliInfoFrom(payload), nil
}

// biliInfoFrom 把播放信息整理为画质选项（按画质号降序，同画质多编码时
// 展示兼容性最好的）。
func biliInfoFrom(p *biliPayload) BilibiliInfo {
	info := BilibiliInfo{
		Title:     p.Title,
		Page:      p.Page,
		Pages:     p.Pages,
		Dash:      p.Dash,
		Qualities: []BiliQuality{},
	}
	audioBandwidth := int64(0)
	if p.Dash {
		if a := pickBiliAudio(p); a != nil {
			audioBandwidth = a.Bandwidth
			info.Audio = biliAudioDesc(a.ID)
		}
	}
	if !p.Dash {
		if len(p.Durl) > 0 {
			info.Qualities = append(info.Qualities, BiliQuality{Desc: "原始画质", Size: p.Durl[0].Size})
		}
		return info
	}
	sec := float64(p.TimeLength) / 1000
	pick := make(map[int]*BiliQuality, len(p.Videos))
	for i := range p.Videos {
		v := p.Videos[i]
		if v.Base == "" {
			continue
		}
		if ex, ok := pick[v.ID]; ok {
			if biliCodecRank(v.Codecs) < biliCodecRank(ex.Codecs) {
				ex.Codecs = v.Codecs
			}
			continue
		}
		q := BiliQuality{ID: v.ID, Desc: biliQualityDesc(v.ID, p.AcceptIDs, p.AcceptDesc), Codecs: v.Codecs}
		if sec > 0 {
			q.Size = int64(float64(v.Bandwidth+audioBandwidth) / 8 * sec)
		}
		pick[v.ID] = &q
	}
	for _, q := range pick {
		info.Qualities = append(info.Qualities, *q)
	}
	sort.Slice(info.Qualities, func(i, j int) bool {
		return info.Qualities[i].ID > info.Qualities[j].ID
	})
	return info
}

// BilibiliDownload 把当前 B 站视频页的指定画质加入下载队列：
// DASH 分轨 → dash 双流任务（合流）；旧版 durl → 普通单文件任务。
// name 为空时按标题/分 P 生成；format 语义与 Tasks.Create 一致（音频
// 格式 = 只提取音轨）。
func (b *Browser) BilibiliDownload(qualityID int, name, format string) error {
	payload, pageURL, err := biliFetch()
	if err != nil {
		return err
	}
	opt := CreateOptions{Name: name, Referer: pageURL}
	opt.UserAgent = siteUserAgent()
	opt.Cookie = cookiesFor(pageURL)

	// 旧版单文件模式：交给通用创建流程（探测、按选择/设置定格式）。
	if !payload.Dash {
		if len(payload.Durl) == 0 || payload.Durl[0].URL == "" {
			return errors.New("未获取到视频地址")
		}
		opt.Format = format
		_, err := createTask(context.Background(), payload.Durl[0].URL, opt)
		return err
	}
	_, err = createBiliDashTask(payload, qualityID, format, pageURL, name, nil)
	return err
}

// createBiliDashTask 从播放信息挑出视频/音频直链并登记 dash 双流任务，
// 返回创建好的任务（供插件 HTTP API 回传）。hdrs 为 nil 时用应用内浏览器
// 会话的 Cookie/UA；插件走 HTTP API 时自带请求头，直接传入。
func createBiliDashTask(p *biliPayload, qualityID int, format, pageURL, name string, hdrs *httpdl.Headers) (engine.Task, error) {
	if mgr == nil || settings == nil {
		return engine.Task{}, ErrNotReady
	}
	s := settings.Get()

	f := strings.ToLower(strings.TrimSpace(format))
	audioOnly := isAudioFormat(f)
	if audioOnly {
		if pickBiliAudio(p) == nil {
			return engine.Task{}, errors.New("该视频没有独立音轨，无法提取音频")
		}
	} else {
		f = resolveFormat(f, "video", s)
		if f == "" {
			f = "mp4" // DASH 必须落一个容器
		}
	}

	videoURL := ""
	if !audioOnly {
		if v := pickBiliVideo(p, qualityID); v != nil {
			videoURL = v.Base
		}
	}
	if videoURL == "" {
		return engine.Task{}, errors.New("未获取到视频流，请刷新页面后重试")
	}

	// 探测最终地址与大小（CDN 302 到带签名的就近节点；签名有时效，
	// 下载时 engine 侧还会再验证）。探测失败不阻塞：按未知大小继续。
	if hdrs == nil {
		hdrs = &httpdl.Headers{Referer: pageURL, UserAgent: siteUserAgent(), Cookie: cookiesFor(pageURL)}
	}
	pctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var firstSize, secondSize int64
	if probe, err := httpdl.ProbeWithHeaders(pctx, videoURL, hdrs); err == nil {
		videoURL, firstSize = probe.URL, probe.Size
	}
	audioURL := ""
	if audio := pickBiliAudio(p); audio != nil {
		audioURL = audio.Base
		if probe, err := httpdl.ProbeWithHeaders(pctx, audioURL, hdrs); err == nil {
			audioURL, secondSize = probe.URL, probe.Size
		}
	} else {
		audioOnly = true // 没有音轨：退化为纯视频单流
	}

	dir := s.DownloadDir
	if err := mkTaskDir(&dir); err != nil {
		return engine.Task{}, err
	}
	if name == "" {
		name = biliDefaultName(p)
	}
	name = swapExt(name, f)
	name = uniqueName(dir, name)

	total := int64(-1)
	switch {
	case audioOnly:
		if secondSize > 0 {
			total = secondSize
		}
	case audioURL == "": // 无独立音轨：纯视频单流
		if firstSize > 0 {
			total = firstSize
		}
	case firstSize > 0 && secondSize > 0:
		total = firstSize + secondSize
	}

	task := engine.Task{
		Kind:       "dash",
		URL:        videoURL,
		Name:       name,
		Dir:        dir,
		Total:      total,
		Threads:    s.Threads,
		Format:     f,
		Referer:    pageURL,
		Cookie:     hdrs.Cookie,
		UserAgent:  hdrs.UserAgent,
		StreamSize: firstSize,
	}
	if audioURL != "" && !audioOnly {
		task.AudioURL = audioURL
		task.AudioSize = secondSize
	}
	if audioOnly {
		// 纯音频任务：第一路就是音轨。
		task.URL = audioURL
		task.StreamSize = secondSize
		task.AudioURL = ""
		task.AudioSize = 0
	}
	created, err := mgr.Create(task)
	if err != nil {
		return engine.Task{}, err
	}
	return created, nil
}

// mkTaskDir 确保下载目录存在（空目录用设置里的下载目录）。
func mkTaskDir(dir *string) error {
	if *dir == "" {
		*dir = settings.Get().DownloadDir
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("无法创建下载目录: %w", err)
	}
	return nil
}

// pickBiliVideo 挑选指定画质的视频流：同画质多编码时取兼容性最好的
// （avc > hevc > av1）；画质不存在时回退码率/画质号最高的一路。
func pickBiliVideo(p *biliPayload, qualityID int) *biliStreamPayload {
	var best, fallback *biliStreamPayload
	for i := range p.Videos {
		v := &p.Videos[i]
		if v.Base == "" {
			continue
		}
		if fallback == nil || v.ID > fallback.ID ||
			(v.ID == fallback.ID && biliCodecRank(v.Codecs) < biliCodecRank(fallback.Codecs)) {
			fallback = v
		}
		if v.ID != qualityID {
			continue
		}
		if best == nil || biliCodecRank(v.Codecs) < biliCodecRank(best.Codecs) {
			best = v
		}
	}
	if best != nil {
		return best
	}
	return fallback
}

// pickBiliAudio 取画质号最高的常规音轨（杜比/Hi-Res 在单独字段，不取）。
func pickBiliAudio(p *biliPayload) *biliStreamPayload {
	var best *biliStreamPayload
	for i := range p.Audios {
		a := &p.Audios[i]
		if a.Base == "" {
			continue
		}
		if best == nil || a.ID > best.ID {
			best = a
		}
	}
	return best
}

// biliCodecRank 编码兼容性排序：avc < hevc < av1（越小越优先，未知最后）。
func biliCodecRank(codec string) int {
	c := strings.ToLower(codec)
	switch {
	case strings.Contains(c, "avc"):
		return 0
	case strings.Contains(c, "hev"), strings.Contains(c, "hvc"):
		return 1
	case strings.Contains(c, "av01"):
		return 2
	}
	return 3
}

// isAudioFormat 报告格式是否为纯音频容器（决定 DASH 任务是否只取音轨）。
func isAudioFormat(f string) bool {
	switch f {
	case "m4a", "mp3", "opus", "flac", "wav":
		return true
	}
	return false
}

var biliQualityNames = map[int]string{
	127: "8K 超高清", 126: "杜比视界", 125: "HDR 真彩", 120: "4K 超清",
	116: "1080P 60帧", 112: "1080P 高码率", 100: "智能修复",
	80: "1080P 高清", 74: "720P 60帧", 64: "720P 高清",
	32: "480P 清晰", 16: "360P 流畅", 6: "240P", 5: "144P",
}

// biliQualityDesc 画质描述：优先页面自带的 accept_description，缺失时用
// 内置映射兜底。
func biliQualityDesc(id int, ids []int, descs []string) string {
	for i, v := range ids {
		if v == id && i < len(descs) && strings.TrimSpace(descs[i]) != "" {
			return descs[i]
		}
	}
	if name, ok := biliQualityNames[id]; ok {
		return name
	}
	return fmt.Sprintf("画质 %d", id)
}

// biliAudioDesc 音轨描述（B 站常规音轨画质号：30216/30232/30280）。
func biliAudioDesc(id int) string {
	switch id {
	case 30280:
		return "高品质音轨"
	case 30232:
		return "中品质音轨"
	case 30216:
		return "标准音轨"
	}
	return "默认音轨"
}

// biliDefaultName 从标题与分 P 生成默认文件名（多 P 视频带 P 序号与分名）。
func biliDefaultName(p *biliPayload) string {
	name := sanitizeTaskName(p.Title)
	if p.Pages > 1 {
		if part := sanitizeTaskName(p.Part); part != "" {
			name = fmt.Sprintf("%s P%d %s", name, p.Page, part)
		} else {
			name = fmt.Sprintf("%s P%d", name, p.Page)
		}
	}
	return name
}

// sanitizeTaskName 把标题净化为安全文件名（规则与 httpdl 的净化一致，
// 截断更短以留出扩展名与去重后缀空间）。
func sanitizeTaskName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case strings.ContainsRune(`\/:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ". ")
	const maxRunes = 150
	if runes := []rune(out); len(runes) > maxRunes {
		out = string(runes[:maxRunes])
	}
	if out == "" {
		out = "bilibili视频"
	}
	return out
}
