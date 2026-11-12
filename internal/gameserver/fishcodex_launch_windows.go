//go:build windows

package gameserver

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 钓鱼宝典（按 F8 呼出的查询面板）跟着游戏客户端走：
// 客户端首次连上来（openSession 的非重连分支）时把它带起来；
// 关闭由工具自己负责——它盯着 rf4_x64，游戏退出后自行结束。
// 服务端只做"拉起"，不去猜工具的存活状态，避免两边互相打断。
//
// 工具位置**不写死绝对路径**：按 gameserver.exe 自己的位置推导。
// 这样换机器、把安装目录挪到别处、甚至改目录名，都不用再改源码重编译。
// 优先级：环境变量 > 相对 exe 推导 > 扫描 tools 下任意子目录。

const fishCodexName = "FishCodex.exe"

var (
	fishCodexMu sync.Mutex
	fishCodexAt time.Time
)

func hiddenProc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// fishCodexCandidates 返回所有可能的工具路径，按优先级排列。
func fishCodexCandidates() []string {
	var out []string

	// 1) 环境变量指定（特殊部署或调试时用，优先级最高）
	if v := strings.TrimSpace(os.Getenv("RF4_FISHCODEX_EXE")); v != "" {
		out = append(out, v)
	}

	exe, err := os.Executable()
	if err != nil {
		return out
	}
	exeDir := filepath.Dir(exe)
	root := filepath.Dir(exeDir) // 约定：<安装根>\bin\gameserver.exe

	// 2) 标准布局：<安装根>\tools\FishCodex\FishCodex.exe
	out = append(out,
		filepath.Join(root, "tools", "FishCodex", fishCodexName),
		filepath.Join(root, "tools", fishCodexName),
		filepath.Join(exeDir, fishCodexName),
		filepath.Join(root, fishCodexName),
	)

	// 3) tools 下任意子目录里找同名 exe，目录被改名也不怕
	if entries, err := os.ReadDir(filepath.Join(root, "tools")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, filepath.Join(root, "tools", e.Name(), fishCodexName))
			}
		}
	}

	return out
}

// resolveFishCodex 逐个探测，返回第一个真实存在的文件路径；都找不到返回空串。
func resolveFishCodex() string {
	for _, p := range fishCodexCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// fishCodexRunning 先查一遍进程，避免重复拉起：
// 第二个实例抢不到 F8 会降级去抢 F1，把游戏自带的帮助键顶掉。
func fishCodexRunning() bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+fishCodexName, "/NH")
	cmd.SysProcAttr = hiddenProc()
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fishCodexName)
}

// launchFishCodex 30 秒去抖，失败也不影响登录流程。
// 找不到工具或拉起失败时写一条 warn，避免像以前那样完全静默。
func launchFishCodex(log *slog.Logger) {
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
		exe := resolveFishCodex()
		if exe == "" {
			if log != nil {
				base, _ := os.Executable()
				log.Warn("钓鱼宝典未找到，跳过拉起", "gameserver", base)
			}
			return
		}
		cmd := exec.Command(exe)
		cmd.Dir = filepath.Dir(exe)
		cmd.SysProcAttr = hiddenProc()
		if err := cmd.Start(); err != nil {
			if log != nil {
				log.Warn("钓鱼宝典拉起失败", "exe", exe, "err", err)
			}
			return
		}
		if log != nil {
			log.Info("钓鱼宝典已拉起", "pid", cmd.Process.Pid, "exe", exe)
		}
	}()
}
