//go:build !windows

package gameserver

import "log/slog"

// 钓鱼宝典只在 Windows 上跑，其他平台这里什么都不做，保持服务端可编译。
func launchFishCodex(*slog.Logger) {}
