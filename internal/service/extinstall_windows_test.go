//go:build windows

package service

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/besscroft/knot/internal/proc"
	"github.com/besscroft/knot/internal/service/extbundle"
)

// 内嵌脚本应包含策略路径与插件 ID，且插值本身不含单引号（会破坏 PS 引号）。
func TestExtPolicyInnerScript(t *testing.T) {
	id := extbundle.Info().ID
	if strings.ContainsAny(id, "'") {
		t.Fatalf("插件 ID 含单引号: %s", id)
	}
	for _, b := range winBrowsers {
		if b.registryKey == "" {
			continue // Firefox 无自动通道，没有策略键
		}
		psPath := extPolicyPSPath(b)
		if strings.ContainsAny(psPath, "'") {
			t.Fatalf("%s 策略路径含单引号: %s", b.id, psPath)
		}
		s := extPolicyInnerScript(psPath, id)
		for _, want := range []string{psPath, id, "exit 0", "New-ItemProperty"} {
			if !strings.Contains(s, want) {
				t.Fatalf("%s 脚本缺少 %q：%s", b.id, want, s)
			}
		}
	}
}

// 外层脚本的内嵌 base64（UTF-16LE）解码后应还原为内嵌脚本。
func TestExtPolicyOuterScriptRoundTrip(t *testing.T) {
	inner := extPolicyInnerScript(`HKCU:\Software\Policies\Google\Chrome\ExtensionInstallAllowlist`, extbundle.Info().ID)
	outer := extPolicyOuterScript(inner)
	i := strings.Index(outer, "-EncodedCommand','")
	if i < 0 {
		t.Fatal("外层脚本缺少 -EncodedCommand")
	}
	start := i + len("-EncodedCommand','")
	end := strings.Index(outer[start:], "'")
	if end < 0 {
		t.Fatal("外层脚本 base64 串未闭合")
	}
	raw, err := base64.StdEncoding.DecodeString(outer[start : start+end])
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	us := make([]uint16, len(raw)/2)
	for j := range us {
		us[j] = uint16(raw[2*j]) | uint16(raw[2*j+1])<<8
	}
	if got := string(utf16.Decode(us)); got != inner {
		t.Fatalf("内嵌脚本还原不符:\n got: %q\nwant: %q", got, inner)
	}
}

// 两个脚本都应通过 PowerShell 语法解析（ParseInput 只解析不执行）。
func TestExtPolicyScriptsParse(t *testing.T) {
	inner := extPolicyInnerScript(`HKCU:\Software\Policies\Google\Chrome\ExtensionInstallAllowlist`, extbundle.Info().ID)
	remover := extPolicyRemoveInnerScript(`HKCU:\Software\Policies\Google\Chrome\ExtensionInstallAllowlist`, extbundle.Info().ID)
	outer := extPolicyOuterScript(inner)
	for i, s := range []string{inner, remover, outer} {
		cmd := proc.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf(`$e = $null; [void][System.Management.Automation.Language.Parser]::ParseInput(@'
%s
'@, [ref]$null, [ref]$e); exit $e.Count`, s))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("脚本 %d 语法错误: %v: %s", i, err, out)
		}
	}
}

// 功能验证：在内嵌脚本上用临时键（不在受限的 Policies 下，免提权）走真实
// 的 -EncodedCommand 通道，覆盖「预置序号 → 追加 → 幂等 → 定点清除」。
// 不弹 UAC，也不碰真实策略键。
func TestExtPolicyInnerScriptFunctional(t *testing.T) {
	base := `HKCU:\Software\KnotSelfTest`
	psPath := base + `\ExtensionInstallAllowlist`
	id := extbundle.Info().ID
	run := func(script string) string {
		t.Helper()
		// stderr 会被 CLIXML 进度记录（首次加载模块）污染，压掉再比较。
		script = "$ProgressPreference = 'SilentlyContinue'\n" + script
		enc := base64.StdEncoding.EncodeToString(utf16LE(script))
		out, err := proc.Command("powershell", "-NoProfile", "-EncodedCommand", enc).CombinedOutput()
		if err != nil {
			t.Fatalf("脚本执行失败: %v: %s", err, out)
		}
		return string(out)
	}
	readKey := func(expr string) string {
		t.Helper()
		return strings.TrimSpace(run(expr))
	}
	t.Cleanup(func() {
		proc.Command("powershell", "-NoProfile", "-Command",
			"Remove-Item -LiteralPath '"+base+"' -Recurse -Force -ErrorAction SilentlyContinue").CombinedOutput()
	})
	// 预置两条已有条目（序号 1、2），验证新写入接在下一位。
	run(fmt.Sprintf(`$ErrorActionPreference = 'Stop'
New-Item -Path '%s' -Force | Out-Null
New-ItemProperty -LiteralPath '%s' -Name '1' -Value '0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' -PropertyType String -Force | Out-Null
New-ItemProperty -LiteralPath '%s' -Name '2' -Value '1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' -PropertyType String -Force | Out-Null`, psPath, psPath, psPath))

	run(extPolicyInnerScript(psPath, id))
	if n := readKey(fmt.Sprintf(`(Get-Item -LiteralPath '%s').GetValueNames().Count`, psPath)); n != "3" {
		t.Fatalf("写入后条目数 = %s, want 3", n)
	}
	if v := readKey(fmt.Sprintf(`(Get-Item -LiteralPath '%s').GetValue('3')`, psPath)); v != id {
		t.Fatalf("序号 3 的值 = %s, want %s", v, id)
	}

	// 再跑一次：已放行应幂等返回，不新增条目。
	run(extPolicyInnerScript(psPath, id))
	if n := readKey(fmt.Sprintf(`(Get-Item -LiteralPath '%s').GetValueNames().Count`, psPath)); n != "3" {
		t.Fatalf("幂等写入后条目数 = %s, want 3", n)
	}

	// 删除脚本应只清插件 ID 的条目、保留预置两条。
	run(extPolicyRemoveInnerScript(psPath, id))
	if n := readKey(fmt.Sprintf(`(Get-Item -LiteralPath '%s').GetValueNames().Count`, psPath)); n != "2" {
		t.Fatalf("删除后条目数 = %s, want 2", n)
	}
	if v := readKey(fmt.Sprintf(`(Get-Item -LiteralPath '%s').GetValue('1')`, psPath)); v != "0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("预置条目 1 被误删: %s", v)
	}
}
