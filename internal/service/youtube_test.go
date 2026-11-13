package service

import "testing"

func TestIsYouTubeVideoPage(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://www.youtube.com/watch?v=abc&t=30s", true},
		{"https://m.youtube.com/watch?v=abc", true},
		{"https://music.youtube.com/watch?v=abc", true},
		{"https://youtu.be/dQw4w9WgXcQ", true},
		{"https://youtu.be/dQw4w9WgXcQ?t=1", true},
		{"https://www.youtube.com/shorts/abc123", true},
		{"https://www.youtube.com/live/abc123", true},
		{"https://www.youtube.com/playlist?list=PLxyz", false},
		{"https://www.youtube.com/watch", false}, // 缺 v 参数
		{"https://www.youtube.com/", false},
		{"https://youtu.be/", false},
		{"https://youtu.be/a/b", false},
		{"https://example.com/watch?v=abc", false},
		{"not a url", false},
	}
	for _, c := range cases {
		if got := isYouTubeVideoPage(c.url); got != c.want {
			t.Errorf("isYouTubeVideoPage(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestIsYouTubeSite(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/playlist?list=PLxyz", true}, // 整域路由：解析层给明确错误
		{"https://youtube.com/watch?v=abc", true},
		{"https://youtu.be/abc", true},
		{"http://example.com", false},
	}
	for _, c := range cases {
		if got := isYouTubeSite(c.url); got != c.want {
			t.Errorf("isYouTubeSite(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}
