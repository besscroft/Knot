// 应用版本与更新入口。检查/安装逻辑在 mygo 的 updater 插件里
// (main.go 的 mygo.Use),插件自带更新窗口与每日自动检查;这里只给
// 前端暴露版本号与手动检查入口(设置页「关于」)。

package service

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// GetVersion 返回当前应用版本号(mygo.config.ts 的 version,构建期注入)。
func (a *AppService) GetVersion() (string, error) {
	return mygo.App.Version(), nil
}

// CheckForUpdate 按用户意图检查更新:插件会立刻弹出更新窗口并展示结果
// (新版本的 release notes / 已是最新 / 失败原因),此调用不阻塞。
func (a *AppService) CheckForUpdate() error {
	updater.CheckForUpdates()
	return nil
}
