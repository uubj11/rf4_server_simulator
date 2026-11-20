// 物品与鱼种图标运行时加载：官方客户端素材不入仓库（CONTRIBUTING 素材红线，
// 同 paper_maps.go 的口径），由 tools/extract_item_icons.py 从客户端导出，
// 再用 tools/pack_item_icons.py 打成部署目录的 ref/icons.zip：
//
//	item_icons/<item_id>.webp   128×128 透明，物品列表与邮件附件用
//	fish_icons/<fish_key>.png   128×64  透明，鱼种列表与加鱼弹窗用
//
// 加载优先级：ref/<类别>/ 散文件（开发期直接放图即可）→ ref/icons.zip 内条目。
// 两者都没有时返回 404，页面 onerror 隐藏 <img>，列表排版不受影响。

package admin

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxIconArchiveBytes   int64  = 128 << 20
	maxIconArchiveEntries        = 20000
	maxIconFileBytes      uint64 = 4 << 20
)

type iconKind struct {
	dir  string
	exts []string
}

// iconKinds 把 URL 中的类别映射到 ref/ 下的素材目录与允许的扩展名。
var iconKinds = map[string]iconKind{
	"items": {dir: "item_icons", exts: []string{".webp", ".png"}},
	"fish":  {dir: "fish_icons", exts: []string{".png", ".webp"}},
}

var iconKindsByDir = func() map[string]iconKind {
	out := make(map[string]iconKind, len(iconKinds))
	for _, spec := range iconKinds {
		out[spec.dir] = spec
	}
	return out
}()

// iconArchive 缓存 ref/icons.zip 的内容索引，首次请求时加载一次。
// 文件在加载后关闭；zip.File 持有内存 ReaderAt，不占用长期文件句柄。
type iconArchive struct {
	once  sync.Once
	files map[string]*zip.File
}

// handleIcons 处理 GET /icons/{kind}/{name}：kind 取 items|fish，
// 先查发布包 ref/<类别>/ 散文件，再查 ref/icons.zip 内 <类别>/<name> 条目。
// 作为静态图片资源不走认证，但只允许白名单类别与图片扩展名。
func (s *Server) handleIcons(w http.ResponseWriter, r *http.Request) {
	kind, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/icons/"), "/")
	spec, known := iconKinds[kind]
	if !ok || !known || !validIconName(name, spec.exts) {
		httpError(w, 404, "图标名不合法")
		return
	}
	if p, found := findRefAsset(spec.dir, name); found {
		setIconHeaders(w, name)
		http.ServeFile(w, r, p)
		return
	}

	s.icons.once.Do(func() {
		p, found := findRefAsset("", "icons.zip")
		if !found {
			return
		}
		files, err := readIconArchive(p)
		if err != nil {
			return
		}
		s.icons.files = files
	})
	f := s.icons.files[spec.dir+"/"+name]
	if f == nil {
		httpError(w, 404, "图标未部署")
		return
	}
	rc, err := f.Open()
	if err != nil {
		httpError(w, 500, "图标读取失败")
		return
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(maxIconFileBytes)+1))
	if err != nil || len(data) == 0 || uint64(len(data)) > maxIconFileBytes {
		httpError(w, 500, "图标读取失败")
		return
	}
	setIconHeaders(w, name)
	http.ServeContent(w, r, name, f.Modified, bytes.NewReader(data))
}

func setIconHeaders(w http.ResponseWriter, name string) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func validIconName(name string, exts []string) bool {
	if name == "" || name == "." || name == ".." || strings.Contains(name, `\`) || name != path.Base(name) {
		return false
	}
	return iconExtAllowed(name, exts)
}

// readIconArchive 读取并校验 ref/icons.zip；损坏、过大或没有有效图标时返回错误。
func readIconArchive(filename string) (map[string]*zip.File, error) {
	return readIconArchiveLimit(filename, maxIconArchiveBytes, maxIconArchiveEntries, maxIconFileBytes)
}

func readIconArchiveLimit(filename string, maxArchiveBytes int64, maxEntries int, maxFileBytes uint64) (map[string]*zip.File, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxArchiveBytes {
		return nil, fmt.Errorf("图标包大小不合法")
	}
	fh, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	raw, err := io.ReadAll(io.LimitReader(fh, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxArchiveBytes {
		return nil, fmt.Errorf("图标包超过大小上限")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	if len(zr.File) > maxEntries {
		return nil, fmt.Errorf("图标包条目过多")
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 || f.UncompressedSize64 > maxFileBytes {
			continue
		}
		if _, ok := normalizeIconArchiveName(f.Name); !ok {
			continue
		}
		files[f.Name] = f
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("图标包没有有效条目")
	}
	return files, nil
}

func normalizeIconArchiveName(name string) (string, bool) {
	if strings.Contains(name, `\`) {
		return "", false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return "", false
	}
	spec, ok := iconKindsByDir[parts[0]]
	if !ok || !validIconName(parts[1], spec.exts) {
		return "", false
	}
	return name, true
}

// iconExtAllowed 限制扩展名，避免通过图标路由探测任意文件。
func iconExtAllowed(name string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// findRefAsset 按 findLocalFile 同源序（工作目录→上级→可执行文件→上级）
// 解析 ref/<sub>/<name> 素材路径；sub 为空时直接解析 ref/<name>。
func findRefAsset(sub, name string) (string, bool) {
	seen := map[string]struct{}{}
	var candidates []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		p := filepath.Clean(filepath.Join(dir, "ref", sub, name))
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		candidates = append(candidates, p)
	}
	if cwd, err := os.Getwd(); err == nil {
		add(cwd)
		add(filepath.Dir(cwd))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		add(dir)
		add(filepath.Dir(dir))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, true
		}
	}
	return "", false
}
