package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"rf4server/internal/config"
)

func writeHosts(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readHosts(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var testDomains = []string{"api.rf4game.ru", "api.rf4game.com"}

func assertBackupExists(t *testing.T, path string, backups []hostsBackup) {
	t.Helper()
	for _, backup := range backups {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), backup.Name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostsBackupCleanupCycles(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	for _, suffix := range []string{"20261114123456", "20261114T123456.000000000", "launcher-unrelated"} {
		if err := os.WriteFile(path+".rf4s-backup-"+suffix, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for cycle := 0; cycle < 3; cycle++ {
		for start := 0; start < 2; start++ {
			if err := hostsSetLocal(path, testDomains); err != nil {
				t.Fatal(err)
			}
		}
		st := readHostsState(path)
		if len(st.Backups) != 2 {
			t.Fatalf("backup count: %+v", st)
		}
		assertBackupExists(t, path, st.Backups)
		if err := hostsRestore(path); err != nil {
			t.Fatal(err)
		}
		for _, backup := range st.Backups {
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), backup.Name)); !os.IsNotExist(err) {
				t.Fatalf("backup remains: %v", err)
			}
		}
		before := readHosts(t, path)
		if err := hostsRestore(path); err != nil {
			t.Fatal(err)
		}
		if readHosts(t, path) != before {
			t.Fatal("restore not idempotent")
		}
		entries, err := filepath.Glob(path + ".rf4s-backup-*")
		if err != nil || len(entries) != 3 {
			t.Fatalf("unexpected backups: %v %v", entries, err)
		}
		for _, name := range entries {
			if readHosts(t, name) != "keep" {
				t.Fatal("unrelated backup changed")
			}
		}
	}
}

func lockHostsAgainstDeletion(t *testing.T, path string) func() {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = windows.CloseHandle(h) }
}

func TestHostsRestoreFailurePreservesBackups(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	unlock := lockHostsAgainstDeletion(t, path)
	err := hostsRestore(path)
	unlock()
	if err == nil {
		t.Fatal("expected restore failure")
	}
	st := readHostsState(path)
	if len(st.Backups) != 2 || len(st.Owned) != 2 {
		t.Fatalf("recovery state lost: %+v", st)
	}
	assertBackupExists(t, path, st.Backups)
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if len(readHostsState(path).Backups) != 0 {
		t.Fatal("recovery did not clean backups")
	}
}

func TestHostsSetLocalStateSaveFailureRemovesBackup(t *testing.T) {
	original := "127.0.0.1 localhost\n"
	path := writeHosts(t, original)
	if err := os.Mkdir(hostsStatePath(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := hostsSetLocal(path, testDomains); err == nil {
		t.Fatal("expected state save failure")
	}
	if got := readHosts(t, path); got != original {
		t.Fatalf("hosts changed after state save failure: %q", got)
	}
	backups, err := filepath.Glob(path + ".rf4s-backup-launcher-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("unregistered backups remain: %v", backups)
	}
}

func TestHostsRestoreStateSaveFailureRemovesBackup(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	before := readHostsState(path)
	unlock := lockHostsAgainstDeletion(t, hostsStatePath(path))
	err := hostsRestore(path)
	unlock()
	if err == nil {
		t.Fatal("expected state save failure")
	}
	after := readHostsState(path)
	if len(after.Backups) != len(before.Backups) {
		t.Fatalf("backup state changed: before=%+v after=%+v", before, after)
	}
	backups, globErr := filepath.Glob(path + ".rf4s-backup-launcher-*")
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(backups) != len(before.Backups) {
		t.Fatalf("unregistered backup remains: %v", backups)
	}
	if hostsMode(path) != "local" {
		t.Fatal("hosts changed before state save succeeded")
	}
}

func TestHostsCleanupFailureWarnsAndRetries(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	backup := readHostsState(path).Backups[0]
	unlock := lockHostsAgainstDeletion(t, filepath.Join(filepath.Dir(path), backup.Name))
	var warnings []string
	err := hostsRestoreWithWarning(path, func(s string) { warnings = append(warnings, s) })
	unlock()
	if err != nil || len(warnings) != 1 || hostsMode(path) != "official" {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	st := readHostsState(path)
	if len(st.Owned) != 0 || len(st.Backups) != 1 {
		t.Fatalf("bad retry state: %+v", st)
	}
	assertBackupExists(t, path, st.Backups)
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if len(readHostsState(path).Backups) != 0 {
		t.Fatal("cleanup retry failed")
	}
}

func TestHostsInterruptedRecovery(t *testing.T) {
	for _, stage := range []string{"before-hosts-write", "after-local", "after-restore-write"} {
		t.Run(stage, func(t *testing.T) {
			path := writeHosts(t, "127.0.0.1 localhost\n")
			original := readHosts(t, path)
			if err := hostsSetLocal(path, testDomains); err != nil {
				t.Fatal(err)
			}
			if stage != "after-local" {
				content := original
				if stage == "after-restore-write" {
					content += "# rf4s-local 127.0.0.1 api.rf4game.ru\n# rf4s-local 127.0.0.1 api.rf4game.com\n"
				}
				// 模拟 hosts 写入前中断，或恢复已写完但清单尚未清理。
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			assertBackupExists(t, path, readHostsState(path).Backups)
			if err := hostsRestore(path); err != nil {
				t.Fatal(err)
			}
			if hostsMode(path) != "official" || len(readHostsState(path).Backups) != 0 {
				t.Fatal("recovery failed")
			}
		})
	}
}

func TestHostsUnconfirmedBackupsPreserved(t *testing.T) {
	for _, kind := range []string{"modified", "manual", "traversal", "directory", "invalid-state"} {
		t.Run(kind, func(t *testing.T) {
			path := writeHosts(t, "127.0.0.1 localhost\n")
			if err := hostsSetLocal(path, testDomains); err != nil {
				t.Fatal(err)
			}
			st := readHostsState(path)
			target := filepath.Join(filepath.Dir(path), st.Backups[0].Name)
			switch kind {
			case "modified":
				if err := os.WriteFile(target, []byte("modified"), 0644); err != nil {
					t.Fatal(err)
				}
			case "manual":
				st.Backups[0].Name = "hosts.rf4s-backup-20261114123456"
			case "traversal":
				st.Backups[0].Name = "../" + st.Backups[0].Name
			case "directory":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := saveHostsState(path, st); err != nil {
				t.Fatal(err)
			}
			if kind == "invalid-state" {
				if err := os.WriteFile(hostsStatePath(path), []byte(`{"owned":["api.rf4game.ru"],"backups":42}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := hostsRestoreWithWarning(path, func(string) {}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("unconfirmed backup removed: %v", err)
			}
		})
	}
}

func TestHostsCleanupWithoutOwnedRows(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 api.rf4game.ru\r\n127.0.0.1 api.rf4game.com\r\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	before := readHosts(t, path)
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if before != readHosts(t, path) || len(readHostsState(path).Backups) != 0 {
		t.Fatal("ownership or cleanup failure")
	}
}

func TestWriteFileAtomicOverwrites(t *testing.T) {
	path := writeHosts(t, "old")

	if err := writeFileAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readHosts(t, path); got != "new" {
		t.Fatalf("内容=%q", got)
	}

	// 替换成功后不应残留临时文件
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

// hosts 位于 System32\drivers\etc，被杀软/索引服务短暂独占是常态。
// 这个用例独占目标文件 30ms，验证替换会重试而不是立刻报错。
func TestWriteFileAtomicRetriesWhenTargetBusy(t *testing.T) {
	path := writeHosts(t, "old")

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = windows.CloseHandle(h)
	}()

	if err := writeFileAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("占用释放后应重试成功，实际失败: %v", err)
	}
	if got := readHosts(t, path); got != "new" {
		t.Fatalf("内容=%q", got)
	}
}

// 证明「单次 MoveFileEx 在目标被独占时确实会失败」，
// 从而说明 replaceFileWithRetry 的重试不是多余的防御。
func TestMoveFileExFailsWhenTargetBusy(t *testing.T) {
	path := writeHosts(t, "old")
	tmp := filepath.Join(filepath.Dir(path), "candidate")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)

	from, _ := windows.UTF16PtrFromString(tmp)
	to, _ := windows.UTF16PtrFromString(path)
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING)
	if err == nil {
		t.Fatal("目标被独占时单次 MoveFileEx 应当失败")
	}
	if !isTransientReplaceError(err) {
		t.Fatalf("应判定为可重试错误，实际: %v", err)
	}
}

func TestHostsSetLocalAppends(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	for _, want := range []string{
		"\n127.0.0.1 api.rf4game.ru",
		"\n127.0.0.1 api.rf4game.com",
		"# RF4 local server",
		"127.0.0.1 localhost",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q:\n%s", want, got)
		}
	}
	// 启用行必须是标准格式（首列 IP），不能带 rf4s-local 前缀
	if strings.Contains(got, "\nrf4s-local") {
		t.Fatalf("启用行格式非法（首列必须是 IP）:\n%s", got)
	}
}

func TestHostsSetLocalIdempotent(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	_ = hostsSetLocal(path, testDomains)
	_ = hostsSetLocal(path, testDomains)
	got := readHosts(t, path)
	if n := strings.Count(got, "127.0.0.1 api.rf4game"); n != 2 {
		t.Fatalf("启用行数量=%d，期望 2:\n%s", n, got)
	}
	if strings.Contains(got, "# rf4s-local") {
		t.Fatalf("不应残留注释行:\n%s", got)
	}
}

func TestHostsRestoreComments(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	_ = hostsSetLocal(path, testDomains)
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if !strings.Contains(got, "# rf4s-local 127.0.0.1 api.rf4game.ru") {
		t.Fatalf("未按 hosts.ps1 约定注释:\n%s", got)
	}
	if hostsMode(path) != "official" {
		t.Fatalf("还原后 mode 应为 official")
	}
	// 幂等：再次还原不报错不变样
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if got != readHosts(t, path) {
		t.Fatal("幂等还原结果不一致")
	}
}

func TestHostsReenableFromCommented(t *testing.T) {
	// 注释态（如手动跑过 hosts.ps1 official）→ 重新启用应恢复标准生效行
	path := writeHosts(t, "# RF4 local server\n# rf4s-local 127.0.0.1 api.rf4game.ru\n# rf4s-local 127.0.0.1 api.rf4game.com\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if strings.Contains(got, "# rf4s-local") {
		t.Fatalf("注释行未被启用:\n%s", got)
	}
	if !strings.Contains(got, "\n127.0.0.1 api.rf4game.com") {
		t.Fatalf("启用行缺失:\n%s", got)
	}
	if hostsMode(path) != "local" {
		t.Fatalf("重新启用后 mode 应为 local")
	}
}

func TestHostsMode(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\n")
	if got := hostsMode(path); got != "official" {
		t.Fatalf("无生效行应视为 official，得到 %s", got)
	}
	_ = hostsSetLocal(path, testDomains)
	if got := hostsMode(path); got != "local" {
		t.Fatalf("启用后应为 local，得到 %s", got)
	}
	_ = hostsRestore(path)
	if got := hostsMode(path); got != "official" {
		t.Fatalf("注释后应为 official，得到 %s", got)
	}
	if got := hostsMode(filepath.Join(t.TempDir(), "missing")); got != "unknown" {
		t.Fatalf("文件缺失应为 unknown，得到 %s", got)
	}
}

func TestHostsAcceptsPs1StyleComment(t *testing.T) {
	// hosts.ps1 写入的注释行带 "# rf4s-local " 前缀，launcher 必须能识别恢复
	path := writeHosts(t, "# rf4s-local 127.0.0.1 api.rf4game.ru\n# rf4s-local 127.0.0.1 api.rf4game.com\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if hostsMode(path) != "local" {
		t.Fatalf("hosts.ps1 注释行未恢复:\n%s", got)
	}
	// 还原后应与 hosts.ps1 的注释格式一致，hosts.ps1 才能继续识别
	_ = hostsRestore(path)
	if !strings.Contains(readHosts(t, path), "# rf4s-local 127.0.0.1 api.rf4game.ru") {
		t.Fatal("还原格式与 hosts.ps1 约定不一致")
	}
}

func TestLoginDomainsDedup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.LoginDomain = "api.rf4game.com"
	if got := loginDomains(cfg); len(got) != 1 {
		t.Fatalf("去重失败: %v", got)
	}
}

func TestHostsRestorePreservesPreexistingActiveLine(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 api.rf4game.ru\r\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if !strings.Contains(got, "127.0.0.1 api.rf4game.ru") {
		t.Fatalf("用户原有生效行不应被改动:\n%s", got)
	}
	if !strings.Contains(got, "# rf4s-local 127.0.0.1 api.rf4game.com") {
		t.Fatalf("launcher 新增行应被还原:\n%s", got)
	}
}

func TestHostsSetLocalCreatesBackupAndState(t *testing.T) {
	original := "127.0.0.1 localhost\r\n"
	path := writeHosts(t, original)
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(path + ".rf4s-backup-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("修改 hosts 前应创建备份")
	}
	if got := readHosts(t, backups[0]); got != original {
		t.Fatalf("备份内容不一致: %q", got)
	}
	st := readHostsState(path)
	if len(st.Owned) != 2 {
		t.Fatalf("归属状态未保存: %+v", st)
	}
}

func TestHostsRestoreClearsOwnershipState(t *testing.T) {
	path := writeHosts(t, "127.0.0.1 localhost\r\n")
	if err := hostsSetLocal(path, testDomains); err != nil {
		t.Fatal(err)
	}
	if err := hostsRestore(path); err != nil {
		t.Fatal(err)
	}
	if st := readHostsState(path); len(st.Owned) != 0 {
		t.Fatalf("还原后归属状态应清空: %+v", st)
	}
}

// 本机 gateway 登录监听在 127.0.0.2:443（让出 127.0.0.1:443 给加速工具），
// 面板不能因为认死 127.0.0.1 就把「已经指向本地」误报成官方服务器。
func TestHostsModeRecognizesConfiguredHost(t *testing.T) {
	path := writeHosts(t, "127.0.0.2 api.rf4game.ru\r\n127.0.0.2 api.rf4game.com\r\n")
	if got := hostsModeFor(path, "127.0.0.2"); got != "local" {
		t.Fatalf("127.0.0.2 生效行应判为本地，实得 %q", got)
	}
	if got := hostsModeFor(path, "127.0.0.1"); got != "official" {
		t.Fatalf("该文件里没有 127.0.0.1 行，不该判成本地，实得 %q", got)
	}
}

// hosts 指向的地址必须跟着 gateway 实际监听地址走，写死 127.0.0.1 会把游戏带到空端口。
func TestLocalHostFollowsLoginAddr(t *testing.T) {
	for _, tc := range []struct{ addr, want string }{
		{"127.0.0.2:443", "127.0.0.2"},
		{"127.0.0.1:443", "127.0.0.1"},
		{":443", "127.0.0.1"},
		{"0.0.0.0:443", "127.0.0.1"},
		{"[::]:443", "127.0.0.1"},
		{"", "127.0.0.1"},
	} {
		cfg := &config.Config{}
		cfg.Gateway.LoginAddr = tc.addr
		if got := localHost(cfg); got != tc.want {
			t.Fatalf("login_addr=%q 应得 %q，实得 %q", tc.addr, tc.want, got)
		}
	}
}

func TestHostsSetLocalHostWritesConfiguredAddress(t *testing.T) {
	path := writeHosts(t, "")
	if err := hostsSetLocalHost(path, testDomains, "127.0.0.2"); err != nil {
		t.Fatal(err)
	}
	got := readHosts(t, path)
	if !strings.Contains(got, "127.0.0.2 api.rf4game.ru") {
		t.Fatalf("应写入 127.0.0.2 行，实际:\n%s", got)
	}
	if mode := hostsModeFor(path, "127.0.0.2"); mode != "local" {
		t.Fatalf("写完后应判为本地，实得 %q", mode)
	}
}
