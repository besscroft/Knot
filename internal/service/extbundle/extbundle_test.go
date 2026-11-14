package extbundle

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// crxField 是手解 protobuf 字段的结果（消息只有两三个字段，无需依赖库）。
type crxField struct {
	tag  int
	body []byte
}

func readProtoField(buf []byte, pos *int) crxField {
	readVarint := func() int {
		v, shift := 0, 0
		for {
			b := buf[*pos]
			*pos++
			v |= int(b&0x7f) << shift
			shift += 7
			if b < 0x80 {
				return v
			}
		}
	}
	tag := readVarint()
	n := readVarint()
	body := buf[*pos : *pos+n]
	*pos += n
	return crxField{tag: tag >> 3, body: body}
}

// TestCrx3 完整校验内嵌 crx3：结构 + 签名（RSA-SHA256 PKCS1v15，覆盖
// "CRX3 SignedData\0" || LE32(signed 长度) || SignedData || zip 原文），
// 与 Chromium crx_verifier.cc 的算法一致。这是对打包脚本的独立交叉验证。
func TestCrx3(t *testing.T) {
	crx := CRX
	if len(crx) < 12 || string(crx[:4]) != "Cr24" {
		t.Fatalf("magic 不符: % x", crx[:4])
	}
	if ver := binary.LittleEndian.Uint32(crx[4:8]); ver != 3 {
		t.Fatalf("version = %d, want 3", ver)
	}
	hLen := int(binary.LittleEndian.Uint32(crx[8:12]))
	if 12+hLen > len(crx) {
		t.Fatalf("header 长度越界: %d", hLen)
	}
	header, archive := crx[12:12+hLen], crx[12+hLen:]

	pos := 0
	fSigned := readProtoField(header, &pos)
	if fSigned.tag != 10000 {
		t.Fatalf("signed_header_data 字段号 = %d, want 10000", fSigned.tag)
	}
	fProof := readProtoField(header, &pos)
	if fProof.tag != 2 {
		t.Fatalf("sha256_with_rsa 字段号 = %d, want 2", fProof.tag)
	}
	if pos != len(header) {
		t.Fatalf("header 有未消费字节: %d/%d", pos, len(header))
	}

	// SignedData{ crx_id }
	ipos := 0
	fCrxID := readProtoField(fSigned.body, &ipos)
	if fCrxID.tag != 1 || len(fCrxID.body) != 16 {
		t.Fatalf("crx_id 字段异常: tag=%d len=%d", fCrxID.tag, len(fCrxID.body))
	}

	// AsymmetricKeyProof{ public_key, signature }
	ipos = 0
	fPub := readProtoField(fProof.body, &ipos)
	fSig := readProtoField(fProof.body, &ipos)
	if fPub.tag != 1 || fSig.tag != 2 {
		t.Fatalf("proof 字段号异常: %d/%d", fPub.tag, fSig.tag)
	}
	pub, err := x509.ParsePKIXPublicKey(fPub.body)
	if err != nil {
		t.Fatalf("公钥解析失败: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("公钥不是 RSA: %T", pub)
	}

	// ID：公钥推导必须与 SignedData 里的 crx_id 一致，且与 meta.json 一致。
	idHex := hex.EncodeToString(fCrxID.body)
	want := ""
	for _, b := range fCrxID.body {
		want += string(rune('a' + b>>4))
		want += string(rune('a' + b&0x0f))
	}
	if DeriveID(fPub.body) != want {
		t.Fatalf("DeriveID 推导不符: %s vs %s", DeriveID(fPub.body), want)
	}
	if Info().ID != want {
		t.Fatalf("meta.json ID %q != crx 派生 %q (hex %s)", Info().ID, want, idHex)
	}

	// 签名验证。
	context := []byte("CRX3 SignedData\x00")
	sizeOctets := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeOctets, uint32(len(fSigned.body)))
	digest := sha256.Sum256(bytes.Join([][]byte{context, sizeOctets, fSigned.body, archive}, nil))
	if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, digest[:], fSig.body); err != nil {
		t.Fatalf("签名验证失败: %v", err)
	}

	// archive 必须是 zip（PK 头）。
	if len(archive) < 2 || archive[0] != 'P' || archive[1] != 'K' {
		t.Fatal("archive 不是 zip")
	}
	if Info().Version == "" {
		t.Fatal("meta.json 缺 version")
	}
}
