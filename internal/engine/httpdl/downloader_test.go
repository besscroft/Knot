package httpdl

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadMultiSegment(t *testing.T) {
	data := randomBytes(t, 96<<10)
	srv := rangeServer(t, data)
	target := filepath.Join(t.TempDir(), "file.bin")

	if err := Download(context.Background(), srv.URL+"/file.bin", target, Options{Threads: 4}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("内容不一致：got %d bytes, want %d bytes", len(got), len(data))
	}
	if _, err := os.Stat(SidecarPath(target)); !os.IsNotExist(err) {
		t.Fatalf("成功后 sidecar 应被删除，got err=%v", err)
	}
}

func TestDownloadSingleStream(t *testing.T) {
	data := randomBytes(t, 32<<10)
	srv := rangeServer(t, data)
	target := filepath.Join(t.TempDir(), "file.bin")

	// Threads=1 且文件小于 MinSegmentSize → 单流。
	if err := Download(context.Background(), srv.URL+"/f.bin", target, Options{Threads: 1}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, data) {
		t.Fatalf("内容不一致")
	}
}

func TestDownloadResumeFromSidecar(t *testing.T) {
	data := randomBytes(t, 64<<10)
	srv := rangeServer(t, data)
	target := filepath.Join(t.TempDir(), "file.bin")

	// 手工构造断点：4 个分片，前两个完成（共 32KB 已写入文件），第三个写了一半。
	if err := os.WriteFile(target, data[:32<<10], 0o644); err != nil {
		t.Fatal(err)
	}
	segs := splitRanges(int64(len(data)), 4)
	segs[0].Written = segs[0].End - segs[0].Start + 1
	segs[1].Written = segs[1].End - segs[1].Start + 1
	segs[2].Written = (segs[2].End - segs[2].Start + 1) / 2
	if err := SaveState(target, &State{URL: srv.URL + "/file.bin", Size: int64(len(data)), ETag: `"abc123"`, Segments: segs}); err != nil {
		t.Fatal(err)
	}

	if err := Download(context.Background(), srv.URL+"/file.bin", target, Options{Threads: 4}); err != nil {
		t.Fatalf("Download(resume): %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, data) {
		t.Fatalf("续传后内容不一致")
	}
}

func TestDownloadResumeRejectsChangedFile(t *testing.T) {
	data := randomBytes(t, 64<<10)
	srv := rangeServer(t, data)
	target := filepath.Join(t.TempDir(), "file.bin")

	// sidecar 记录的大小与服务器不符 → 应忽略断点、重新完整下载。
	segs := splitRanges(int64(len(data)), 4)
	if err := SaveState(target, &State{URL: srv.URL, Size: 123, Segments: segs}); err != nil {
		t.Fatal(err)
	}
	if err := Download(context.Background(), srv.URL+"/file.bin", target, Options{Threads: 4}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, data) {
		t.Fatalf("服务器内容变化时应重新完整下载")
	}
}

func TestDownloadCancelKeepsSidecar(t *testing.T) {
	data := randomBytes(t, 64<<10)
	srv := rangeServer(t, data)
	target := filepath.Join(t.TempDir(), "file.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	if err := Download(ctx, srv.URL+"/file.bin", target, Options{Threads: 4}); err == nil {
		t.Fatal("ctx 已取消时 Download 应返回错误")
	}
}

func TestSplitRanges(t *testing.T) {
	cases := []struct {
		size    int64
		n       int
		wantN   int
		wantEnd int64
	}{
		{size: 100, n: 4, wantN: 4, wantEnd: 99},
		{size: 101, n: 4, wantN: 4, wantEnd: 100}, // 余数归最后一段
		{size: 3, n: 8, wantN: 3, wantEnd: 2},     // 段数不超过大小
	}
	for _, c := range cases {
		segs := splitRanges(c.size, c.n)
		if len(segs) != c.wantN {
			t.Fatalf("splitRanges(%d,%d) 段数 = %d, want %d", c.size, c.n, len(segs), c.wantN)
		}
		if segs[0].Start != 0 || segs[len(segs)-1].End != c.wantEnd {
			t.Fatalf("splitRanges(%d,%d) 区间错误: %+v", c.size, c.n, segs)
		}
		// 相邻且无缝
		for i := 1; i < len(segs); i++ {
			if segs[i].Start != segs[i-1].End+1 {
				t.Fatalf("分片不连续: %+v", segs)
			}
		}
		// 覆盖完整
		var total int64
		for _, s := range segs {
			total += s.End - s.Start + 1
		}
		if total != c.size {
			t.Fatalf("分片总长 %d != %d", total, c.size)
		}
	}
}

func TestProbeFilename(t *testing.T) {
	cases := []struct {
		disp, rawURL, ctype, want string
	}{
		{`attachment; filename="hello.zip"`, "http://x/a", "", "hello.zip"},
		{"attachment; filename*=UTF-8''%E4%B8%AD%E6%96%87.txt", "http://x/a", "", "中文.txt"},
		{"", "http://x/path/to/photo%20one.png", "", "photo one.png"},
		{"", "http://x/", "application/pdf", "download.pdf"},
		{"", "http://x/", "application/octet-stream", "download"},
		{`attachment; filename="bad:name?.zip"`, "http://x/a", "", "bad_name_.zip"},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.rawURL)
		_ = u
		got := FileName(c.disp, c.rawURL, c.ctype)
		if got != c.want {
			t.Errorf("FileName(%q,%q,%q) = %q, want %q", c.disp, c.rawURL, c.ctype, got, c.want)
		}
	}
}

func TestParseContentRangeTotal(t *testing.T) {
	cases := map[string]int64{
		"bytes 0-0/12345": 12345,
		"bytes 0-0/*":     -1,
		"garbage":         -1,
	}
	for in, want := range cases {
		if got := parseContentRangeTotal(in); got != want {
			t.Errorf("parseContentRangeTotal(%q) = %d, want %d", in, got, want)
		}
	}
}
