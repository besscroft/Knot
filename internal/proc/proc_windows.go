//go:build windows

// Package proc 统一包装子进程启动：GUI 程序在 Windows 上派生控制台程序
// （yt-dlp/ffmpeg/reg/tasklist 等）时，子进程默认会新建控制台并显示出来，
// 用户看到黑框一闪而过甚至随任务常驻。这里统一阻止控制台分配；管道
// （CombinedOutput/Output/Stdout 重定向）不受影响。
package proc

import (
	"context"
	"os/exec"
	"syscall"
)

// creationFlags = CREATE_NO_WINDOW：不为子进程创建控制台窗口。
const creationFlags = 0x0800_0000

func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: creationFlags}
	return cmd
}

func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: creationFlags}
	return cmd
}
