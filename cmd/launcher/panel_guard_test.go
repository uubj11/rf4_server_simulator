package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePidFile(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"18120", 18120},
		{"  18120\n", 18120},
		{"18120\r\n", 18120},
		{"", 0},
		{"   ", 0},
		{"abc", 0},
		{"0", 0},
		{"-3", 0},
	}
	for _, c := range cases {
		if got := parsePidFile([]byte(c.in)); got != c.want {
			t.Fatalf("parsePidFile(%q) 应为 %d，实得 %d", c.in, c.want, got)
		}
	}
}

func newTestGuard(t *testing.T, procs map[uint32]string, pidContent string) (*panelGuard, *int, *string) {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "growth_panel_watch.pid")
	if pidContent != "" {
		if err := os.WriteFile(pidFile, []byte(pidContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	lastLog := ""
	g := &panelGuard{
		pidFile: pidFile,
		procFn:  func() map[uint32]string { return procs },
		runTask: func() error { calls++; return nil },
	}
	g.log = &stringLogger{buf: &lastLog}
	return g, &calls, &lastLog
}

type stringLogger struct{ buf *string }

func (l *stringLogger) Info(s string)  { *l.buf = "INFO " + s }
func (l *stringLogger) Warn(s string)  { *l.buf = "WARN " + s }
func (l *stringLogger) Error(s string) { *l.buf = "ERR " + s }

// 游戏没跑：一次都不该拉起校准（否则就变成开机自启了）。
func TestPanelGuardIdleWithoutGame(t *testing.T) {
	g, calls, _ := newTestGuard(t, map[uint32]string{100: "explorer.exe"}, "")
	for i := 0; i < 3; i++ {
		if g.tick() {
			t.Fatal("游戏没跑却拉起了校准")
		}
	}
	if *calls != 0 {
		t.Fatalf("游戏没跑却调了 %d 次 schtasks", *calls)
	}
}

// 游戏在跑、校准没在跑：拉起一次；校准活着时不再重复拉起。
func TestPanelGuardStartsOncePerGameSession(t *testing.T) {
	g, calls, logBuf := newTestGuard(t, map[uint32]string{
		100: "explorer.exe",
		200: "rf4_x64.exe",
	}, "")
	if !g.tick() {
		t.Fatal("游戏在跑但没拉起校准")
	}
	if *calls != 1 {
		t.Fatalf("应调用一次 schtasks，实得 %d", *calls)
	}
	if !strings.Contains(*logBuf, "已随游戏拉起") {
		t.Fatalf("日志缺拉起记录: %q", *logBuf)
	}

	// 校准已经起来了（pid 文件 + pythonw 进程都在）：不重复拉起
	dir := filepath.Dir(g.pidFile)
	if err := os.WriteFile(g.pidFile, []byte("300"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = dir
	g.procFn = func() map[uint32]string {
		return map[uint32]string{200: "rf4_x64.exe", 300: "pythonw.exe"}
	}
	if g.tick() {
		t.Fatal("校准已在跑却又拉起了一次")
	}
	if *calls != 1 {
		t.Fatalf("schtasks 被重复调用: %d", *calls)
	}
}

// pid 文件里是死号（进程已退出）或者被别的程序复用：都要重新拉起。
func TestPanelGuardRestartsWhenCalibrationDied(t *testing.T) {
	g, calls, _ := newTestGuard(t, map[uint32]string{
		200: "rf4_x64.exe",
	}, "999")
	if !g.tick() {
		t.Fatal("校准 pid 已失效却没有重新拉起")
	}
	if *calls != 1 {
		t.Fatalf("应调用一次 schtasks，实得 %d", *calls)
	}

	// pid 被非 python 进程复用（比如系统复用了 999）→ 仍视为没在跑
	g.procFn = func() map[uint32]string {
		return map[uint32]string{200: "rf4_x64.exe", 999: "svchost.exe"}
	}
	if !g.tick() {
		t.Fatal("pid 被别的进程复用后应重新拉起校准")
	}
	if *calls != 2 {
		t.Fatalf("应调用两次 schtasks，实得 %d", *calls)
	}
}

// 拉起失败要记日志，且同一错误不刷屏。
func TestPanelGuardLogsFailureOnce(t *testing.T) {
	g, _, logBuf := newTestGuard(t, map[uint32]string{200: "rf4_x64.exe"}, "")
	g.runTask = func() error { return errors.New("拒绝访问") }
	if g.tick() {
		t.Fatal("失败时不该报告拉起成功")
	}
	if !strings.Contains(*logBuf, "WARN") || !strings.Contains(*logBuf, "拒绝访问") {
		t.Fatalf("失败没有落日志: %q", *logBuf)
	}
}
