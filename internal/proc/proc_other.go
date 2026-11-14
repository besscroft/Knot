//go:build !windows

// 非 Windows 平台无控制台弹窗问题，直通 exec。
package proc

import (
	"context"
	"os/exec"
)

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func Command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
