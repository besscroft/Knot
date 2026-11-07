package hls

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pkcs7Pad 按 HLS 规范给分片补填充。
func pkcs7Pad(b []byte) []byte {
	pad := 16 - len(b)%16
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

// encryptAES128 用 AES-128-CBC + PKCS7 加密（测试服务器用）。
func encryptAES128(plain, key, iv []byte) []byte {
	block, _ := aes.NewCipher(key)
	padded := pkcs7Pad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

// hlsServer 搭一个迷你 HLS 站点：
//
//	/master.m3u8        → 两个变体（低带宽在前，选 highest 时应选中 high）
//	/low.m3u8 /high.m3u8 → media playlist（3 个分片；high 的第 2 片 AES-128 加密）
//	/seg{n}.ts /key
func hlsServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	highPlain0 := []byte("HIGH-SEG-0-CONTENT_______________")
	highPlain1 := []byte("HIGH-SEG-1-ENCRYPTED-CONTENT_____")
	highPlain2 := []byte("HIGH-SEG-2-CONTENT_______________")
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(0xA0 + i)
	}
	highEnc1 := encryptAES128(highPlain1, key, iv)

	master := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=400000\nlow.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2000000\nhigh.m3u8\n"
	highList := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-ENDLIST\n" +
		"#EXTINF:4.0,\nseg0.ts\n" +
		"#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x" + hexEnc(iv) + "\n" +
		"#EXTINF:4.0,\nseg1.ts\n" +
		"#EXT-X-KEY:METHOD=NONE\n" +
		"#EXTINF:4.0,\nseg2.ts\n"
	lowList := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n" +
		"#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-ENDLIST\n" +
		"#EXTINF:4.0,\nseg0.ts\n"

	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(master)) })
	mux.HandleFunc("/high.m3u8", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(highList)) })
	mux.HandleFunc("/low.m3u8", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(lowList)) })
	mux.HandleFunc("/key", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(key) })
	mux.HandleFunc("/seg0.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(highPlain0) })
	mux.HandleFunc("/seg1.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(highEnc1) })
	mux.HandleFunc("/seg2.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(highPlain2) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func hexEnc(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0x0f])
	}
	return string(out)
}
