// hosts.go —— hosts_switch 功能：launcher 启动服务后把登录域名指向
// 127.0.0.1，退出时还原，省去手动编辑 hosts 的步骤。
//
// 只还原 launcher 自己启用的行：归属状态保存在 hosts 同目录的 sidecar 文件，
// 不会把用户原本已有的 127.0.0.1 映射误当作 launcher 管理项。
//
// 写 hosts 需要管理员权限；失败时调用方记录告警，不阻断服务启动。
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"rf4server/internal/config"
)

const (
	hostsSection  = "# RF4 local server"
	hostsMarker   = "# rf4s-local"
	hostsLocalRow = "127.0.0.1 "
)

// localHost 返回 hosts 该指向的本机地址：跟着 gateway 的登录监听走。
// 本服为把 127.0.0.1:443 让给加速/302 工具，登录监听写成 127.0.0.2:443，
// 此时 hosts 也必须写 127.0.0.2，否则游戏会连到一个没人监听的端口。
func localHost(cfg *config.Config) string {
	if cfg == nil {
		return "127.0.0.1"
	}
	addr := strings.TrimSpace(cfg.Gateway.LoginAddr)
	if addr == "" {
		return "127.0.0.1"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	switch host {
	case "", "0.0.0.0", "::", "localhost", "localhost.localdomain":
		return "127.0.0.1"
	}
	return host
}

// hostsState 记录 launcher 实际启用的域名。状态写在 hosts 文件旁，随退出
// 或下一次启动恢复，崩溃后也能识别归属。
type hostsState struct {
	Owned   []string      `json:"owned,omitempty"`
	Backups []hostsBackup `json:"backups,omitempty"`
}

type hostsBackup struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// 面板切换与退出恢复共用状态文件，必须串行更新归属和备份清单。
var hostsMu sync.Mutex

// 归属行/注释行的正则按目标地址生成：本机可能把登录监听放在 127.0.0.2，
// 认死 127.0.0.1 会让面板把「已经指向本地」误报成官方服务器。
func hostsActiveRe(host string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(host) + `\s+(api\.rf4game\.(?:ru|com))\s*$`)
}

func hostsCommentRe(host string) *regexp.Regexp {
	return regexp.MustCompile(`^#\s*rf4s-local\s+` + regexp.QuoteMeta(host) + `\s+(api\.rf4game\.(?:ru|com))`)
}

// hostsFilePath 返回系统 hosts 文件路径。
func hostsFilePath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root + `\System32\drivers\etc\hosts`
}

// loginDomains 返回需要指向本机的登录域名（主域名 + 备用 com 域，去重）。
func loginDomains(cfg *config.Config) []string {
	ru, com := cfg.Gateway.LoginDomain, "api.rf4game.com"
	if ru == com {
		return []string{ru}
	}
	return []string{ru, com}
}

func hostsStatePath(path string) string {
	return path + ".rf4s-state.json"
}

func readHostsState(path string) hostsState {
	var st hostsState
	data, err := os.ReadFile(hostsStatePath(path))
	if err == nil {
		if json.Unmarshal(data, &st) != nil {
			return hostsState{}
		}
	}
	return st
}

func saveHostsState(path string, st hostsState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(hostsStatePath(path), data, 0o600)
}

func backupHosts(path string) (hostsBackup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hostsBackup{}, err
	}
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".rf4s-backup-launcher-"+time.Now().UTC().Format("20060102T150405.000000000")+"-*")
	if err != nil {
		return hostsBackup{}, err
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(f.Name())
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return hostsBackup{}, err
	}
	if _, err := f.Write(data); err != nil {
		return hostsBackup{}, err
	}
	if err := f.Sync(); err != nil {
		return hostsBackup{}, err
	}
	if err := f.Close(); err != nil {
		return hostsBackup{}, err
	}
	complete = true
	return hostsBackup{Name: filepath.Base(f.Name()), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}

// saveHostsStateAfterBackup 保存状态；失败时删除尚未登记的备份，避免永久残留。
func saveHostsStateAfterBackup(path string, st hostsState, backup hostsBackup) error {
	if err := saveHostsState(path, st); err != nil {
		if cleanupErr := removeHostsBackup(path, backup); cleanupErr != nil {
			return fmt.Errorf("保存 hosts 状态失败: %w；删除未登记备份失败: %v", err, cleanupErr)
		}
		return err
	}
	return nil
}

// writeFileAtomic 先写同目录临时文件，再通过 Windows MoveFileEx 替换目标。
// 这样 hosts 或状态文件不会在写入中途被截断。
func writeFileAtomic(path string, data []byte, mode fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return replaceFileWithRetry(from, to)
}

// replaceFileWithRetry 用 MoveFileEx 替换目标文件，遇到共享冲突时短暂重试。
//
// hosts 位于 System32\drivers\etc，常被杀软、Search 索引或 DNS Client 短暂持有。
// 单次替换失败会让面板随机报「hosts 写入失败」，而那时通常是毫秒级占用，
// 因此对「文件被占用」这类可恢复错误做有限重试；权限不足等错误不重试。
func replaceFileWithRetry(from, to *uint16) error {
	const attempts = 5
	var err error
	for i := 0; i < attempts; i++ {
		err = windows.MoveFileEx(from, to,
			windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			return nil
		}
		if !isTransientReplaceError(err) {
			return err
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(20*(i+1)) * time.Millisecond) // 20/40/60/80ms
		}
	}
	return err
}

// isTransientReplaceError 判断错误是否属于「等一下就好」的类型。
func isTransientReplaceError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// hostsSetLocal 将本项目管理的行切换为启用状态（标准 hosts 格式，指向
// 127.0.0.1）。用户原本已有的同域名映射保持不变且不会被标记为 launcher
// 所有；只有本项目新增或从 rf4s-local 注释恢复的行才会在退出时还原。
func hostsSetLocal(path string, domains []string) error {
	return hostsSetLocalHost(path, domains, "127.0.0.1")
}

// hostsSetLocalHost 同 hostsSetLocal，但生效行写成调用方指定的本机地址
// （本服登录监听在 127.0.0.2:443 时要写 127.0.0.2）。
func hostsSetLocalHost(path string, domains []string, host string) error {
	if host == "" {
		host = "127.0.0.1"
	}
	activeRe := hostsActiveRe(host)
	commentRe := hostsCommentRe(host)
	row := host + " "
	hostsMu.Lock()
	defer hostsMu.Unlock()
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	oldState := readHostsState(path)
	oldOwned := make(map[string]bool, len(oldState.Owned))
	for _, d := range oldState.Owned {
		oldOwned[d] = true
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	requested := make(map[string]bool, len(domains))
	for _, d := range domains {
		if d != "" {
			requested[d] = true
		}
	}
	active := make(map[string]bool, len(domains))
	for _, line := range lines {
		if m := activeRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			active[m[1]] = true
		}
	}

	out := make([]string, 0, len(lines)+len(domains)+2)
	owned := make(map[string]bool, len(domains))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := activeRe.FindStringSubmatch(trimmed); m != nil {
			domain := m[1]
			out = append(out, trimmed)
			if requested[domain] && oldOwned[domain] {
				owned[domain] = true
			}
			continue
		}
		if m := commentRe.FindStringSubmatch(trimmed); m != nil {
			domain := m[1]
			if requested[domain] && !active[domain] {
				out = append(out, row+domain)
				active[domain] = true
				owned[domain] = true
				continue
			}
			if requested[domain] && active[domain] {
				continue
			}
		}
		out = append(out, strings.TrimRight(line, "\r"))
	}

	sectionAdded := false
	for _, d := range domains {
		if d == "" || active[d] {
			continue
		}
		if !sectionAdded {
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, hostsSection)
			sectionAdded = true
		}
		out = append(out, row+d)
		active[d] = true
		owned[d] = true
	}
	state := hostsState{Owned: make([]string, 0, len(owned)), Backups: oldState.Backups}
	for _, d := range domains {
		if owned[d] {
			state.Owned = append(state.Owned, d)
			delete(owned, d)
		}
	}
	for d := range owned {
		state.Owned = append(state.Owned, d)
	}

	backup, err := backupHosts(path)
	if err != nil {
		return err
	}
	state.Backups = append(state.Backups, backup)
	// 先保存归属，再改 hosts；即使随后写 hosts 失败，下次启动也不会误认用户行。
	if err := saveHostsStateAfterBackup(path, state, backup); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(strings.Join(out, "\r\n")), 0o644)
}

// hostsMode 判断当前 hosts 指向：local=存在生效的标准格式行；official=
// 生效行全部处于注释状态或不存在；unknown=读取失败。
func hostsMode(path string) string {
	return hostsModeFor(path, "127.0.0.1")
}

// hostsModeFor 按指定本机地址判断指向：local=存在该地址的生效行；
// official=生效行全部处于注释状态或不存在；unknown=读取失败。
func hostsModeFor(path, host string) string {
	if host == "" {
		host = "127.0.0.1"
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	re := hostsActiveRe(host)
	for _, line := range strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n") {
		if re.MatchString(strings.TrimSpace(line)) {
			return "local"
		}
	}
	return "official"
}

// hostsRestore 只注释 launcher 归属状态中记录的行，恢复官方解析。幂等。
func hostsRestore(path string) error {
	return hostsRestoreWithWarning(path, panelLogger{}.Warn)
}

// hostsRestoreHost 同 hostsRestore，但按调用方指定的本机地址识别归属行。
func hostsRestoreHost(path, host string) error {
	return hostsRestoreHostWithWarning(path, host, panelLogger{}.Warn)
}

func hostsRestoreWithWarning(path string, warn func(string)) error {
	return hostsRestoreHostWithWarning(path, "127.0.0.1", warn)
}

func hostsRestoreHostWithWarning(path, host string, warn func(string)) error {
	if host == "" {
		host = "127.0.0.1"
	}
	activeRe := hostsActiveRe(host)
	hostsMu.Lock()
	defer hostsMu.Unlock()
	st := readHostsState(path)
	if len(st.Owned) == 0 && len(st.Backups) == 0 {
		return nil
	}
	owned := make(map[string]bool, len(st.Owned))
	for _, d := range st.Owned {
		owned[d] = true
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := activeRe.FindStringSubmatch(trimmed); m != nil && owned[m[1]] {
			lines[i] = hostsMarker + " " + trimmed
			changed = true
		}
	}
	if changed {
		backup, err := backupHosts(path)
		if err != nil {
			return err
		}
		st.Backups = append(st.Backups, backup)
		// 恢复前登记备份，进程中断后仍可继续恢复和清理。
		if err := saveHostsStateAfterBackup(path, st, backup); err != nil {
			return err
		}
		if err := writeFileAtomic(path, []byte(strings.Join(lines, "\r\n")), 0o644); err != nil {
			return err
		}
	}
	st.Owned = nil
	if err := saveHostsState(path, st); err != nil {
		warn("hosts 已还原，但归属状态保存失败，保留备份: " + err.Error())
		return nil
	}
	remaining := st.Backups[:0]
	for _, backup := range st.Backups {
		if err := removeHostsBackup(path, backup); err != nil {
			warn("hosts 已还原，但备份清理失败: " + err.Error())
			remaining = append(remaining, backup)
		}
	}
	st.Backups = remaining
	if err := saveHostsState(path, st); err != nil {
		warn("hosts 已还原，但备份清理状态保存失败: " + err.Error())
	}
	return nil
}

// 只删除清单中位于同目录、名称及内容均匹配的 launcher 普通文件。
// 旧版本或手动脚本生成的备份没有这份归属证据，始终保留。
func removeHostsBackup(path string, backup hostsBackup) error {
	if filepath.Base(backup.Name) != backup.Name || strings.ContainsAny(backup.Name, `/\:`) ||
		!strings.HasPrefix(backup.Name, filepath.Base(path)+".rf4s-backup-launcher-") {
		return fmt.Errorf("无法确认备份归属: %q", backup.Name)
	}
	target := filepath.Join(filepath.Dir(path), backup.Name)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("备份不是普通文件: %s", target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if backup.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		return fmt.Errorf("备份内容已改变: %s", target)
	}
	return os.Remove(target)
}
