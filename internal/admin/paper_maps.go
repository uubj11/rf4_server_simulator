// 纸地图贴图运行时加载：官方客户端贴图不入仓库（CONTRIBUTING 素材红线，
// 同 docs/fish-query-water.md 水层掩码口径），由 tools/extract_paper_maps.py
// 导出到发布包 ref/paper_maps/ 后从这里加载；缺图时返回合成占位 SVG，
// 工作台的几何功能（区号网格、标定、区域绘制）不依赖贴图即可使用。

package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// handleMapTextures GET /maps/{name}.webp：优先发布包 ref/paper_maps/ 下的
// 同名贴图，缺失时合成占位图。与内嵌 SPA 的 FileServer 同为静态资源，不走认证。
func (s *Server) handleMapTextures(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/maps/")
	if name == "" || name != filepath.Base(name) || !strings.HasSuffix(name, ".webp") ||
		!strings.HasPrefix(name, "map_") {
		httpError(w, 404, "贴图名不合法")
		return
	}
	if path, ok := findPaperMap(name); ok {
		w.Header().Set("Content-Type", "image/webp")
		http.ServeFile(w, r, path)
		return
	}
	paperMapPlaceholder(w, name)
}

// findPaperMap 与 findLocalFile 同源序（工作目录→上级→可执行文件→上级）解析贴图。
func findPaperMap(name string) (string, bool) {
	seen := map[string]struct{}{}
	var candidates []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		path := filepath.Clean(filepath.Join(dir, "ref", "paper_maps", name))
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
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
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// paperMapPlaceholder 合成占位：全部纸地图都是 2048×2048（fishmaps Scenes/SeaPaper），
// 深底网格让标定坐标读数可目测核对，文字提示部署路径。svg 必须带显式
// width/height，否则没有固有尺寸，<img> 侧 naturalWidth 恒为 0（工作台与
// UI 测试都以解码成功为前提）。name 来自 URL，进入 XML 文本节点前做转义。
func paperMapPlaceholder(w http.ResponseWriter, name string) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg" width="2048" height="2048" viewBox="0 0 2048 2048">`)
	b.WriteString(`<rect width="2048" height="2048" fill="#1b2430"/>`)
	for i := 0; i <= 16; i++ {
		p := strconv.Itoa(i * 128)
		b.WriteString(`<line x1="` + p + `" y1="0" x2="` + p + `" y2="2048" stroke="#2c3a4a" stroke-width="2"/>`)
		b.WriteString(`<line x1="0" y1="` + p + `" x2="2048" y2="` + p + `" stroke="#2c3a4a" stroke-width="2"/>`)
	}
	b.WriteString(`<text x="1024" y="960" fill="#8aa2b8" font-size="72" text-anchor="middle" font-family="sans-serif">纸地图素材未部署</text>`)
	b.WriteString(`<text x="1024" y="1080" fill="#61788e" font-size="48" text-anchor="middle" font-family="sans-serif">ref/paper_maps/` + xmlEscape(name) + `</text>`)
	b.WriteString(`<text x="1024" y="1180" fill="#61788e" font-size="40" text-anchor="middle" font-family="sans-serif">python tools/extract_paper_maps.py（见 docs/admin-console.md）</text>`)
	b.WriteString(`</svg>`)
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

// xmlEscape 只做 XML 文本节点必需的三个实体：贴图名已由路由约束为
// filepath.Base + map_ 前缀 + .webp 后缀，不会再进属性位置。
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
