package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 成长套装面板强度是客户端按静态定义算出来的，服务端下发不进去（见经验总结
// §3.34.22）。现役做法是 tools\growth_panel_watch.py 在游戏运行期用内存补丁把
// 面板强度改成成长目标值。它以前只由「启动游戏（Steam版）.cmd」用
// schtasks /run 拉起，玩家从 Steam 库直接启动就整条链路被绕过，面板必然回退。
//
// 这里把这件事收到 launcher：launcher 本来就是常驻进程（计划任务
// RF4_ServerLauncher），每 5 秒看一眼系统进程，发现 rf4_x64.exe 而校准不在跑，
// 就用 schtasks 拉起 RF4_GrowthPanelWatcher（任务本身是 Highest，命令行窗口
// 用 CREATE_NO_WINDOW 隐藏，不闪黑框）。游戏退出后校准进程按 EXIT_GRACE 自己
// 退出，下一局重新被拉起 —— 完全跟随游戏启停，不需要开机自启，也不再挑启动入口。
const (
	growthGameExe     = "rf4_x64.exe"
	growthWatcherTask = "RF4_GrowthPanelWatcher"
	growthWatcherPid  = `tools\growth_panel_watch.pid`
	growthGuardPeriod = 5 * time.Second
)

// parsePidFile 读 pid 锁文件里的进程号；空文件/脏内容一律当 0（视为校准没在跑）。
func parsePidFile(data []byte) uint32 {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0
	}
	// 只取第一行，容忍尾部换行与多余空白
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil || v == 0 {
		return 0
	}
	return uint32(v)
}

// snapshotProcesses 枚举当前进程，返回 pid -> 小写镜像名。
func snapshotProcesses() map[uint32]string {
	out := make(map[uint32]string, 256)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return out
	}
	for {
		name := pe.ExeFile
		n := 0
		for n < len(name) && name[n] != 0 {
			n++
		}
		out[pe.ProcessID] = strings.ToLower(syscall.UTF16ToString(name[:n]))
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out
}

// panelGuard 决定「这一轮该不该拉起校准进程」。
// procFn / runTask 可替换，便于在没有真实游戏进程时验证判定逻辑。
type panelGuard struct {
	pidFile string
	procFn  func() map[uint32]string
	runTask func() error
	log     logger
	lastErr string
}

// tick 执行一次判定。返回 true 表示本轮真的拉起了校准。
func (g *panelGuard) tick() bool {
	procs := g.procFn()
	gameRunning := false
	for _, name := range procs {
		if name == growthGameExe {
			gameRunning = true
			break
		}
	}
	if !gameRunning {
		return false
	}
	if g.watcherAlive(procs) {
		return false
	}
	if err := g.runTask(); err != nil {
		msg := err.Error()
		if msg != g.lastErr && g.log != nil {
			g.log.Warn("拉起成长套装面板校准失败: " + msg)
			g.lastErr = msg
		}
		return false
	}
	g.lastErr = ""
	if g.log != nil {
		g.log.Info("已随游戏拉起成长套装面板校准（" + growthWatcherTask + "）")
	}
	return true
}

// watcherAlive 判定校准进程是否还活着：pid 锁文件里的进程必须还在，
// 且镜像名是 python/py 系（防止 pid 被系统复用后误判）。
func (g *panelGuard) watcherAlive(procs map[uint32]string) bool {
	data, err := os.ReadFile(g.pidFile)
	if err != nil {
		return false
	}
	pid := parsePidFile(data)
	if pid == 0 {
		return false
	}
	name, ok := procs[pid]
	if !ok {
		return false
	}
	base := strings.TrimSuffix(name, ".exe")
	return strings.HasPrefix(base, "python")
}

// fileLogger 把看护结论落到 logs/growth_panel_guard.log。
// launcher 是 windowsgui 程序，stdout 看不到，必须自己写文件才好排查。
type fileLogger struct {
	path string
	mu   sync.Mutex
}

func (l *fileLogger) write(level, msg string) {
	if l == nil || l.path == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)
}

func (l *fileLogger) Info(m string)  { l.write("INFO", m) }
func (l *fileLogger) Warn(m string)  { l.write("WARN", m) }
func (l *fileLogger) Error(m string) { l.write("ERROR", m) }

// startPanelGuard 起常驻循环：launcher 活多久就守多久。
func (a *app) startPanelGuard(ctx context.Context) {
	root := filepath.Dir(filepath.Dir(mustExePath()))
	g := &panelGuard{
		pidFile: filepath.Join(root, growthWatcherPid),
		procFn:  snapshotProcesses,
		runTask: runGrowthWatcherTask,
		log:     &fileLogger{path: filepath.Join(root, "logs", "growth_panel_guard.log")},
	}
	// 先给游戏一点启动时间，避免 launcher 刚起来就空转
	ticker := time.NewTicker(growthGuardPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.tick()
		}
	}
}

// runGrowthWatcherTask 用计划任务拉起校准进程，窗口全程隐藏。
func runGrowthWatcherTask() error {
	exe := "schtasks"
	if root := os.Getenv("SystemRoot"); root != "" {
		candidate := filepath.Join(root, "System32", "schtasks.exe")
		if _, err := os.Stat(candidate); err == nil {
			exe = candidate
		}
	}
	cmd := exec.Command(exe, "/run", "/TN", growthWatcherTask)
	// HideWindow + CREATE_NO_WINDOW：schtasks 本身是控制台程序，不隐藏会闪黑框
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}
