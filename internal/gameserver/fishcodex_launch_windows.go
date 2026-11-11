//go:build windows

package gameserver

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 钓鱼宝典（按 F8 呼出的查询面板）跟着游戏客户端走：
// 客户端首次连上来（openSession 的非重连分支）时把它带起来；
// 关闭由工具自己负责——它盯着 rf4_x64，游戏退出后自行结束。
// 服务端只做"拉起"，不去猜工具的存活状态，避免两边互相打断。
const (
	fishCodexExe = `D:\02_游戏开发\RF4本地服\tools\FishCodex\FishCodex.exe`
	fishCodexDir = `D:\02_游戏开发\RF4本地服\tools\FishCodex`
)

var (
	fishCodexMu sync.Mutex
	fishCodexAt time.Time
)

func hiddenProc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// fishCodexRunning 先查一遍进程，避免重复拉起：
// 第二个实例抢不到 F8 会降级去抢 F1，把游戏自带的帮助键顶掉。
func fishCodexRunning() bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq FishCodex.exe", "/NH")
	cmd.SysProcAttr = hiddenProc()
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "FishCodex.exe")
}

// launchFishCodex 30 秒去抖，失败也不影响登录流程（工具起不来只是按 F8 没反应）。
func launchFishCodex() {
	fishCodexMu.Lock()
	if !fishCodexAt.IsZero() && time.Since(fishCodexAt) < 30*time.Second {
		fishCodexMu.Unlock()
		return
	}
	fishCodexAt = time.Now()
	fishCodexMu.Unlock()

	go func() {
		if fishCodexRunning() {
			return
		}
		cmd := exec.Command(fishCodexExe)
		cmd.Dir = fishCodexDir
		cmd.SysProcAttr = hiddenProc()
		_ = cmd.Start()
	}()
}
