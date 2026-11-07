package service

import "testing"

func TestSanitizeWebSettings(t *testing.T) {
	// 首次运行：端口回默认。
	var v Settings
	sanitizeSettings(&v, "d")
	if v.WebPort != DefaultWebPort {
		t.Errorf("默认端口应为 %d，got %d", DefaultWebPort, v.WebPort)
	}
	// 已有合法值：保持不变。
	v2 := v
	v2.WebPort = 22333
	sanitizeSettings(&v2, "d")
	if v2.WebPort != 22333 {
		t.Errorf("合法端口不应被改写，got %d", v2.WebPort)
	}
	// 越界端口回默认。
	v3 := v
	v3.WebPort = 80
	sanitizeSettings(&v3, "d")
	if v3.WebPort != DefaultWebPort {
		t.Errorf("越界端口应回默认，got %d", v3.WebPort)
	}
}
