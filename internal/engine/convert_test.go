package engine

import (
	"strings"
	"testing"
)

func TestConvertArgs(t *testing.T) {
	cases := map[string][]string{
		"mkv":  {"-map", "0", "-c", "copy", "-f", "matroska"},
		"mov":  {"-map", "0", "-c", "copy", "-f", "mov"},
		"m4a":  {"-vn", "-c:a", "copy", "-f", "ipod"},
		"mp3":  {"-vn", "-c:a", "libmp3lame", "-q:a", "2", "-f", "mp3"},
		"flac": {"-vn", "-c:a", "flac", "-f", "flac"},
		"wav":  {"-vn", "-c:a", "pcm_s16le", "-f", "wav"},
		"opus": {"-vn", "-c:a", "libopus", "-b:a", "160k", "-f", "ogg"},
	}
	for format, want := range cases {
		args, _ := convertArgs(format)
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Errorf("convertArgs(%q) = %v, want %v", format, args, want)
		}
	}
	// webm 允许回退（源编码不兼容时重编码）；mp4 无需转换参数。
	if _, mayFallback := convertArgs("webm"); !mayFallback {
		t.Error("webm 应标记可回退")
	}
	if args, _ := convertArgs("mp4"); args != nil {
		t.Errorf("mp4 不应需要转换参数, got %v", args)
	}
}

func TestFinalizeSkipsNonMedia(t *testing.T) {
	// 未知/空格式：直接原样返回，不触发 FFmpeg 查找。
	got, err := Finalize(t.Context(), "C:\\tmp\\a.zip", "")
	if err != nil || got != "C:\\tmp\\a.zip" {
		t.Fatalf("空格式应原样返回: %v %v", got, err)
	}
	got, err = Finalize(t.Context(), "C:\\tmp\\a.zip", "7z")
	if err != nil || got != "C:\\tmp\\a.zip" {
		t.Fatalf("不支持的格式应原样返回: %v %v", got, err)
	}
}
