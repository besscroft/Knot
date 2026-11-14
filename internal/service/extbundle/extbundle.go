// Package extbundle 内嵌浏览器插件的分发产物（由 extension/scripts/
// pack-crx.mjs 在构建时同步到本目录并提交）：crx3 安装包、解压目录
// 的 zip（手动加载兜底）与元信息（扩展 ID / 版本）。
// 更新插件后运行 `bun run sync:ext` 重新生成本目录内容。
package extbundle

import (
	_ "embed"
	"crypto/sha256"
	"encoding/json"
	"strings"
)

//go:embed knot-extension.crx
var CRX []byte

//go:embed knot-extension.zip
var ZIP []byte

//go:embed meta.json
var metaJSON []byte

// Meta 是打包脚本生成的元信息：扩展 ID（由打包密钥派生，稳定不变）
// 与 manifest 版本号。
type Meta struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Info 返回内嵌插件的元信息；meta.json 由构建链生成，缺失视为空。
func Info() Meta {
	var m Meta
	_ = json.Unmarshal(metaJSON, &m)
	return m
}

// DeriveID 从公钥（SPKI DER）推导扩展 ID：SHA256 前 16 字节按 nibble
// 映射 a-p（crx_file::id_util::GenerateId）。打包脚本与注册表键共用。
func DeriveID(pubKey []byte) string {
	sum := sha256.Sum256(pubKey)
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		sb.WriteByte('a' + (sum[i] >> 4))
		sb.WriteByte('a' + (sum[i] & 0x0f))
	}
	return sb.String()
}
