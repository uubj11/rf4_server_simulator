package admin

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

type zipEntry struct {
	name string
	body []byte
}

func testIconZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			t.Fatalf("创建 zip 条目 %q: %v", entry.name, err)
		}
		if _, err := w.Write(entry.body); err != nil {
			t.Fatalf("写入 zip 条目 %q: %v", entry.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip: %v", err)
	}
	return buf.Bytes()
}

func writeTestIconZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "icons.zip")
	if err := os.WriteFile(name, testIconZip(t, entries), 0o600); err != nil {
		t.Fatalf("写入测试 zip: %v", err)
	}
	return name
}

func loadTestIconFiles(t *testing.T) map[string]*zip.File {
	t.Helper()
	name := writeTestIconZip(t, []zipEntry{
		{name: "item_icons/bait.webp", body: []byte("webp-data")},
		{name: "fish_icons/cod.png", body: []byte("png-data")},
	})
	files, err := readIconArchive(name)
	if err != nil {
		t.Fatalf("读取测试图标包: %v", err)
	}
	return files
}

func TestReadIconArchiveFiltersUnsafeEntries(t *testing.T) {
	name := writeTestIconZip(t, []zipEntry{
		{name: "item_icons/bait.webp", body: []byte("webp-data")},
		{name: "fish_icons/cod.png", body: []byte("png-data")},
		{name: "item_icons/zero.webp"},
		{name: "item_icons/../escape.webp", body: []byte("bad")},
		{name: "item_icons/sub/nested.webp", body: []byte("bad")},
		{name: "item_icons/readme.txt", body: []byte("bad")},
		{name: `fish_icons\bad.png`, body: []byte("bad")},
		{name: "other/data.png", body: []byte("bad")},
	})
	files, err := readIconArchive(name)
	if err != nil {
		t.Fatalf("读取图标包: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("只应保留安全图标条目，实得 %d: %#v", len(files), files)
	}
	for _, key := range []string{"item_icons/bait.webp", "fish_icons/cod.png"} {
		if files[key] == nil {
			t.Fatalf("缺少安全条目 %q", key)
		}
	}
	for _, key := range []string{"item_icons/../escape.webp", "item_icons/sub/nested.webp", "item_icons/zero.webp"} {
		if files[key] != nil {
			t.Fatalf("不应接受条目 %q", key)
		}
	}
}

func TestReadIconArchiveLimits(t *testing.T) {
	entries := []zipEntry{
		{name: "item_icons/a.webp", body: []byte("12345678")},
		{name: "fish_icons/b.png", body: []byte("12345678")},
	}
	raw := testIconZip(t, entries)
	name := filepath.Join(t.TempDir(), "icons.zip")
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readIconArchiveLimit(name, int64(len(raw)-1), 100, 1024); err == nil {
		t.Fatal("应拒绝超过压缩包大小上限的文件")
	}
	if _, err := readIconArchiveLimit(name, int64(len(raw)), 1, 1024); err == nil {
		t.Fatal("应拒绝条目数超过上限的文件")
	}
	if _, err := readIconArchiveLimit(name, int64(len(raw)), 100, 4); err == nil {
		t.Fatal("全部条目超过单项大小上限时应拒绝空图标包")
	}
}

func TestReadIconArchiveRejectsCorruptFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "icons.zip")
	if err := os.WriteFile(name, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readIconArchive(name); err == nil {
		t.Fatal("损坏的图标包应返回错误")
	}
}

func newIconHandlerServer(t *testing.T, files map[string]*zip.File) *Server {
	t.Helper()
	s := NewServer(config.Admin{JWTSecret: "icon-test"}, nil, logging.Discard(), nil)
	s.icons.once.Do(func() { s.icons.files = files })
	return s
}

func TestHandleIconsServesArchiveEntry(t *testing.T) {
	s := newIconHandlerServer(t, loadTestIconFiles(t))
	rec := httptest.NewRecorder()
	s.handleIcons(rec, httptest.NewRequest(http.MethodGet, "/icons/items/bait.webp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("图标应返回 200，实得 %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=86400") {
		t.Fatalf("Cache-Control = %q", got)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if string(body) != "webp-data" {
		t.Fatalf("响应体 = %q", body)
	}
}

func TestHandleIconsRejectsInvalidNamesAndMissingArchive(t *testing.T) {
	s := NewServer(config.Admin{JWTSecret: "icon-test"}, nil, logging.Discard(), nil)
	s.icons.once.Do(func() {})
	for _, requestPath := range []string{
		"/icons/items/../secret.webp",
		"/icons/items/%2e%2e%2fsecret.webp",
		"/icons/items/sub/icon.webp",
		"/icons/items/icon.gif",
		"/icons/unknown/icon.webp",
		"/icons/items/",
	} {
		rec := httptest.NewRecorder()
		s.handleIcons(rec, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("非法路径 %q 应返回 404，实得 %d", requestPath, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.handleIcons(rec, httptest.NewRequest(http.MethodGet, "/icons/items/bait.webp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺包应返回 404，实得 %d", rec.Code)
	}
}

func TestHandleIconsConcurrentArchiveReads(t *testing.T) {
	s := newIconHandlerServer(t, loadTestIconFiles(t))
	const workers = 32
	errs := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.handleIcons(rec, httptest.NewRequest(http.MethodGet, "/icons/fish/cod.png", nil))
			if rec.Code != http.StatusOK || rec.Body.String() != "png-data" {
				errs <- rec.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatalf("并发读取失败: %q", msg)
	}
}
