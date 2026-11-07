package service

import (
	"encoding/json"
	"testing"
)

func TestIsBilibiliVideoPage(t *testing.T) {
	yes := []string{
		"https://www.bilibili.com/video/BV166nW6REyR/",
		"https://www.bilibili.com/video/BV166nW6REyR?p=3",
		"https://m.bilibili.com/video/BV166nW6REyR",
		"https://bilibili.com/video/BV166nW6REyR",
	}
	no := []string{
		"https://www.bilibili.com/",
		"https://www.bilibili.com/anime/123",
		"https://www.bilibili.com.evil.com/video/BV1",
		"https://example.com/video/BV1",
		"",
	}
	for _, u := range yes {
		if !isBilibiliVideoPage(u) {
			t.Errorf("isBilibiliVideoPage(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if isBilibiliVideoPage(u) {
			t.Errorf("isBilibiliVideoPage(%q) = true, want false", u)
		}
	}
}

func TestPickBiliVideo(t *testing.T) {
	p := &biliPayload{Videos: []biliStreamPayload{
		{ID: 80, Codecs: "av01.0.08M.08", Base: "av01-url"},
		{ID: 80, Codecs: "hev1.1.6.L150.90", Base: "hev-url"},
		{ID: 80, Codecs: "avc1.64040A", Base: "avc-url"},
		{ID: 64, Codecs: "avc1.6401F", Base: "low-url"},
	}}
	if v := pickBiliVideo(p, 80); v.Base != "avc-url" {
		t.Errorf("80P 应选兼容性最好的 avc, got %s", v.Base)
	}
	if v := pickBiliVideo(p, 64); v.Base != "low-url" {
		t.Errorf("64P 应选对应画质, got %s", v.Base)
	}
	// 画质不存在时回退最高画质；同画质回退时也取 avc。
	if v := pickBiliVideo(p, 127); v.Base != "avc-url" {
		t.Errorf("缺失画质应回退最高且取 avc, got %s", v.Base)
	}
	// 只有 hev 的画质（如 4K HDR）也能选中。
	h := &biliPayload{Videos: []biliStreamPayload{
		{ID: 125, Codecs: "dvh1.05.06", Base: "dolby"},
		{ID: 125, Codecs: "hev1.1.6.L150.90", Base: "hev"},
	}}
	if v := pickBiliVideo(h, 125); v.Base != "hev" {
		t.Errorf("125 应在 hev/dvh1 中取 hev, got %s", v.Base)
	}
}

func TestPickBiliAudio(t *testing.T) {
	p := &biliPayload{Audios: []biliStreamPayload{
		{ID: 30216, Base: "low"}, {ID: 30280, Base: "high"}, {ID: 30232, Base: "mid"},
	}}
	if a := pickBiliAudio(p); a.ID != 30280 {
		t.Errorf("应取最高音轨 30280, got %d", a.ID)
	}
	if a := pickBiliAudio(&biliPayload{}); a != nil {
		t.Error("无音轨应返回 nil")
	}
}

func TestBiliInfoFrom(t *testing.T) {
	p := &biliPayload{
		Dash: true, TimeLength: 60_000, // 60 秒
		Title: "测试视频", Page: 2, Pages: 3, Part: "第二集",
		AcceptIDs:  []int{80, 64, 32},
		AcceptDesc: []string{"高清 1080P", "高清 720P", "清晰 480P"},
		Videos: []biliStreamPayload{
			{ID: 80, Codecs: "avc1.64040A", Bandwidth: 2_000_000, Base: "v1"},
			{ID: 80, Codecs: "av01.0.08M.08", Bandwidth: 1_500_000, Base: "v2"},
			{ID: 64, Codecs: "avc1.6401F", Bandwidth: 1_000_000, Base: "v3"},
		},
		Audios: []biliStreamPayload{{ID: 30280, Bandwidth: 320_000, Base: "a1"}},
	}
	info := biliInfoFrom(p)
	if !info.Dash || info.Title != "测试视频" || info.Audio != "高品质音轨" {
		t.Fatalf("基础信息不正确: %+v", info)
	}
	if len(info.Qualities) != 2 {
		t.Fatalf("应去重为 2 个画质, got %d", len(info.Qualities))
	}
	q0, q1 := info.Qualities[0], info.Qualities[1]
	if q0.ID != 80 || q0.Desc != "高清 1080P" || q0.Codecs != "avc1.64040A" {
		t.Errorf("80P 描述/编码不正确: %+v", q0)
	}
	// 大小 = (视频码率 + 音频码率)/8 × 60s，音频取最高音轨。
	if q0.Size != (2_000_000+320_000)/8*60 {
		t.Errorf("80P 估算大小不正确: %d", q0.Size)
	}
	if q1.ID != 64 || q1.Size != (1_000_000+320_000)/8*60 {
		t.Errorf("64P 不正确: %+v", q1)
	}
}

func TestBiliDefaultName(t *testing.T) {
	p := &biliPayload{Title: `大肥鱼/适合做:女朋友吗？`, Pages: 3, Page: 2, Part: `还有"小笼包"的事`}
	got := biliDefaultName(p)
	want := `大肥鱼_适合做_女朋友吗？ P2 还有_小笼包_的事`
	if got != want {
		t.Errorf("biliDefaultName = %q, want %q", got, want)
	}
	single := biliDefaultName(&biliPayload{Title: "单P视频", Pages: 1})
	if single != "单P视频" {
		t.Errorf("单 P 不应带序号: %q", single)
	}
}

func TestHasStreams(t *testing.T) {
	if (&biliPayload{OK: true}).hasStreams() {
		t.Error("空壳 playinfo 不应报告有流")
	}
	if !(&biliPayload{Dash: true, Videos: []biliStreamPayload{{ID: 80, Base: "u"}}}).hasStreams() {
		t.Error("DASH 有视频流应报告有流")
	}
	if !(&biliPayload{Durl: []biliDurlItem{{URL: "u"}}}).hasStreams() {
		t.Error("durl 有条目应报告有流")
	}
}

func TestBiliPayloadFromAPI(t *testing.T) {
	data := &biliAPIData{}
	if err := json.Unmarshal([]byte(`{
	  "dash": {
	    "video": [
	      {"id": 80, "codecs": "avc1.64040A", "bandwidth": 2000000, "base_url": "https://v/avc.m4s", "backup_url": ["https://b/1"]},
	      {"id": 80, "codecs": "av01.0.08M.08", "bandwidth": 1500000, "base_url": "https://v/av01.m4s"}
	    ],
	    "audio": [{"id": 30280, "codecs": "mp4a.40.2", "bandwidth": 320000, "base_url": "https://a/1.m4s"}]
	  },
	  "durl": null,
	  "accept_quality": [80, 64],
	  "accept_description": ["高清 1080P", "高清 720P"],
	  "timelength": 61000
	}`), data); err != nil {
		t.Fatal(err)
	}
	p := biliPayloadFromAPI(data)
	if !p.OK || !p.Dash || !p.hasStreams() {
		t.Fatalf("转换结果不正确: %+v", p)
	}
	if len(p.Videos) != 2 || p.Videos[0].Base != "https://v/avc.m4s" {
		t.Errorf("视频流转换不正确: %+v", p.Videos)
	}
	if len(p.Audios) != 1 || p.Audios[0].ID != 30280 {
		t.Errorf("音频流转换不正确: %+v", p.Audios)
	}
	if len(p.AcceptIDs) != 2 || p.AcceptIDs[0] != 80 || p.AcceptDesc[1] != "高清 720P" || p.TimeLength != 61000 {
		t.Errorf("画质/时长转换不正确: %+v", p)
	}
}

func TestBiliVideoIDFromURL(t *testing.T) {
	cases := map[string]string{
		"https://www.bilibili.com/video/BV166nW6REyR/?p=2": "BV166nW6REyR",
		"https://www.bilibili.com/video/av170001":          "av170001",
		"https://www.bilibili.com/":                        "",
		"https://example.com/video/BV1abc":                 "",
	}
	for u, want := range cases {
		if got := biliVideoIDFromURL(u); got != want {
			t.Errorf("biliVideoIDFromURL(%q) = %q, want %q", u, got, want)
		}
	}
}
