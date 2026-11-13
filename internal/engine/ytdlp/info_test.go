package ytdlp

import (
	"encoding/json"
	"testing"
)

// sampleJSON 是一段贴近真实输出的 -J 片段（横屏 1080p 视频：avc/vp9/av1
// 三种编码、音轨 m4a/opus 各一路、360p 渐进式合并格式）。
const sampleJSON = `{
  "_type": "video",
  "id": "dQw4w9WgXcQ",
  "title": "测试视频",
  "duration": 212.5,
  "is_live": false,
  "formats": [
    {"format_id": "137", "width": 1920, "height": 1080, "fps": 30, "vcodec": "avc1.640028", "acodec": "none", "ext": "mp4", "filesize": 89128960, "tbr": 3325.0, "protocol": "https"},
    {"format_id": "248", "width": 1920, "height": 1080, "fps": 30, "vcodec": "vp09.00.50.08", "acodec": "none", "ext": "webm", "filesize": 73400320, "tbr": 2739.0, "protocol": "https"},
    {"format_id": "313", "width": 3840, "height": 2160, "fps": 30, "vcodec": "vp09.00.50.08", "acodec": "none", "ext": "webm", "filesize": 268435456, "tbr": 10068.0, "protocol": "https"},
    {"format_id": "299", "width": 1920, "height": 1080, "fps": 60, "vcodec": "avc1.64002a", "acodec": "none", "ext": "mp4", "filesize": 99614720, "tbr": 3725.0, "protocol": "https"},
    {"format_id": "140", "vcodec": "none", "acodec": "mp4a.40.2", "ext": "m4a", "filesize": 3400000, "tbr": 128.0, "protocol": "https"},
    {"format_id": "251", "vcodec": "none", "acodec": "opus", "ext": "webm", "filesize": 2700000, "tbr": 160.0, "protocol": "https"},
    {"format_id": "18", "width": 640, "height": 360, "fps": 30, "vcodec": "avc1.42001E", "acodec": "mp4a.40.2", "ext": "mp4", "filesize_approx": 10485760, "tbr": 395.0, "protocol": "https"},
    {"format_id": "93", "width": 1280, "height": 720, "fps": 30, "vcodec": "avc1.640029", "acodec": "none", "ext": "mp4", "tbr": 1300.0, "protocol": "m3u8_native"}
  ]
}`

func loadSample(t *testing.T) *Info {
	t.Helper()
	var raw jsonInfo
	if err := json.Unmarshal([]byte(sampleJSON), &raw); err != nil {
		t.Fatalf("解析样例 JSON: %v", err)
	}
	return infoFrom(&raw)
}

func TestInfoFromBuckets(t *testing.T) {
	info := loadSample(t)
	if info.Title != "测试视频" || info.ID != "dQw4w9WgXcQ" || info.Live {
		t.Fatalf("元数据不符: %+v", info)
	}
	// m3u8 的 720p 被排除；档位：2160/1080/360（1080 的 30fps 与 60fps
	// 合并为一档，60fps 的 299 码率更高且同为 avc 编码）。
	if len(info.Qualities) != 3 {
		t.Fatalf("档位数 = %d, want 3: %+v", len(info.Qualities), info.Qualities)
	}
	if info.Qualities[0].ID != 2160 || info.Qualities[1].ID != 1080 || info.Qualities[2].ID != 360 {
		t.Fatalf("档位顺序不符: %+v", info.Qualities)
	}
}

func TestInfoFromCodecPreference(t *testing.T) {
	info := loadSample(t)
	q1080 := info.Quality(1080)
	if q1080 == nil {
		t.Fatal("缺少 1080p 档位")
	}
	// 同档 avc 优先于 vp9；1080p60 的 avc 码率也更高。
	if q1080.videoID != "299" {
		t.Fatalf("1080p 采用格式 = %s, want 299 (avc 高码率)", q1080.videoID)
	}
	if q1080.Desc != "1080p60" || q1080.Fps != 60 || q1080.Codecs != "H.264" {
		t.Fatalf("1080p 描述不符: %+v", q1080)
	}
	// 大小 = 视频 + 码率最高的音轨（251 opus）。
	if want := int64(99614720 + 2700000); q1080.Size != want {
		t.Fatalf("1080p 大小 = %d, want %d", q1080.Size, want)
	}
	// 2160p 只有 vp9；360p 是渐进式合并格式（音轨内嵌，audioID 空）。
	if q2160 := info.Quality(2160); q2160.Codecs != "VP9" || q2160.audioID != "251" {
		t.Fatalf("2160p 不符: %+v", q2160)
	}
	if q360 := info.Quality(360); q360.videoID != "18" || q360.audioID != "" || q360.Size != 10485760 {
		t.Fatalf("360p 渐进式不符: %+v", q360)
	}
}

func TestInfoQualityFallback(t *testing.T) {
	info := loadSample(t)
	// 档位不存在时回退最高档。
	if q := info.Quality(999); q == nil || q.ID != 2160 {
		t.Fatalf("回退档位不符: %+v", q)
	}
}

func TestFormatSelector(t *testing.T) {
	info := loadSample(t)
	if got := info.FormatSelector(1080, false); got != "299+251/bv*[height<=1080]+ba/b[height<=1080]" {
		t.Fatalf("1080p 选择器 = %q", got)
	}
	// 档位 0 不存在：回退最高档的精确格式 + 高度兜底链。
	if got := info.FormatSelector(0, false); got != "313+251/bv*[height<=2160]+ba/b[height<=2160]" {
		t.Fatalf("回退选择器 = %q", got)
	}
	if got := info.FormatSelector(1080, true); got != "251/ba/b" {
		t.Fatalf("仅音频选择器 = %q", got)
	}
}

func TestFormatSelectorNoQualities(t *testing.T) {
	info := &Info{}
	if got := info.FormatSelector(1080, false); got != "bv*+ba/b" {
		t.Fatalf("空画质选择器 = %q", got)
	}
	if got := info.FormatSelector(1080, true); got != "ba/b" {
		t.Fatalf("空画质仅音频选择器 = %q", got)
	}
}

func TestInfoPortraitShorts(t *testing.T) {
	// 竖屏短片：短边（宽度）作为档位，用户感知与横屏一致。
	raw := &jsonInfo{Title: "竖屏", Duration: 30, Formats: []jsonFormat{
		{FormatID: "sb2", Width: 1080, Height: 1920, VCodec: "vp09.00.50.08", ACodec: "none", Ext: "webm", Filesize: 100, TBR: 100},
		{FormatID: "140", VCodec: "none", ACodec: "mp4a.40.2", Ext: "m4a", Filesize: 10, TBR: 128},
	}}
	info := infoFrom(raw)
	if len(info.Qualities) != 1 || info.Qualities[0].ID != 1080 {
		t.Fatalf("竖屏档位不符: %+v", info.Qualities)
	}
}

func TestFormatSizeFallback(t *testing.T) {
	// filesize 与 approx 都缺失时用码率×时长估算（tbr 千字节/秒）。
	f := jsonFormat{FormatID: "x", TBR: 800}
	if got := formatSize(f, 10); got != 1_000_000 { // 800*1000/8*10
		t.Fatalf("码率估算 = %d, want 1000000", got)
	}
	if got := formatSize(jsonFormat{}, 10); got != 0 {
		t.Fatalf("无信息估算 = %d, want 0", got)
	}
}
