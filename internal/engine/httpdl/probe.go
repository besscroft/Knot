package httpdl

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// ProbeResult 是对下载地址的探测结果。
type ProbeResult struct {
	URL          string // 跟随重定向后的最终地址
	Size         int64  // 总字节数，未知为 -1
	AcceptRanges bool   // 服务器是否支持 Range 分片
	FileName     string // 从 Content-Disposition 或 URL 推断
	ContentType  string
	ETag         string
}

// probe 用带 Range 的 GET 请求探测目标（不少服务器对 HEAD 的处理与 GET 不一致）。
func probe(ctx context.Context, client *http.Client, rawURL string, headers *Headers) (*ProbeResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效的 URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("暂只支持 http/https 链接")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", "bytes=0-0")
	headers.Apply(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}()

	res := &ProbeResult{URL: resp.Request.URL.String(), Size: -1}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		res.AcceptRanges = true
		res.Size = parseContentRangeTotal(resp.Header.Get("Content-Range"))
	case http.StatusOK:
		if resp.ContentLength >= 0 {
			res.Size = resp.ContentLength
		}
		res.AcceptRanges = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
	default:
		return nil, fmt.Errorf("服务器返回 %s", resp.Status)
	}
	res.ContentType = resp.Header.Get("Content-Type")
	res.ETag = resp.Header.Get("ETag")
	res.FileName = FileName(resp.Header.Get("Content-Disposition"), res.URL, res.ContentType)
	return res, nil
}

// parseContentRangeTotal 从 "bytes 0-0/12345" 中取总大小，无法解析时返回 -1。
func parseContentRangeTotal(v string) int64 {
	_, total, ok := strings.Cut(v, "/")
	if !ok {
		return -1
	}
	total = strings.TrimSpace(total)
	if total == "*" {
		return -1
	}
	var n int64
	if _, err := fmt.Sscanf(total, "%d", &n); err != nil {
		return -1
	}
	return n
}

// FileName 依次从 Content-Disposition（含 RFC 5987 的 filename*）、URL 路径推断文件名，
// 再按 ContentType 兜底，最终净化为安全的文件名。
func FileName(contentDisposition, rawURL, contentType string) string {
	name := fileNameFromDisposition(contentDisposition)
	if name == "" {
		name = fileNameFromURL(rawURL)
	}
	if name == "" {
		name = fileNameFromContentType(contentType)
	}
	return sanitize(name)
}

func fileNameFromDisposition(v string) string {
	if v == "" {
		return ""
	}
	// filename*=UTF-8''%E4%B8%AD.txt 优先（RFC 5987）。
	for _, part := range strings.Split(v, ";") {
		part = strings.TrimSpace(part)
		if val, ok := strings.CutPrefix(part, "filename*="); ok {
			if _, decoded, ok := strings.Cut(val, "''"); ok {
				if unescaped, err := url.PathUnescape(decoded); err == nil && unescaped != "" {
					return unescaped
				}
			}
		}
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	return params["filename"]
}

func fileNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return name
}

func fileNameFromContentType(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	if ext, ok := knownExtensions[mt]; ok {
		return "download" + ext
	}
	return ""
}

var knownExtensions = map[string]string{
	"application/zip":          ".zip",
	"application/gzip":         ".gz",
	"application/x-tar":        ".tar",
	"application/pdf":          ".pdf",
	"application/json":         ".json",
	"image/jpeg":               ".jpg",
	"image/png":                ".png",
	"image/gif":                ".gif",
	"image/webp":               ".webp",
	"video/mp4":                ".mp4",
	"video/x-matroska":         ".mkv",
	"audio/mpeg":               ".mp3",
	"text/html":                ".html",
	"text/plain":               ".txt",
	"application/octet-stream": "",
}

// sanitize 去掉路径分隔符与控制字符，截断过长的名字。
func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	name = strings.Trim(name, ". ")
	const maxRunes = 180
	if runes := []rune(name); len(runes) > maxRunes {
		name = string(runes[:maxRunes])
	}
	if name == "" {
		return "download"
	}
	return name
}
