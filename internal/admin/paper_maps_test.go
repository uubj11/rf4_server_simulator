package admin

import (
	"net/http/httptest"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

// /maps/ 贴图路由：缺图回合成占位 SVG（素材不入仓库）。占位必须带显式
// width/height（无固有尺寸则 <img> naturalWidth 恒为 0，工作台/测试等不到
// 解码成功），名字来自 URL，写入 XML 文本节点前必须转义。
func TestPaperMapPlaceholder(t *testing.T) {
	srv := NewServer(config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"},
		testDB(t), logging.Discard(), WebFS())

	get := func(name string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleMapTextures(rec, httptest.NewRequest("GET", "/maps/"+name, nil))
		return rec
	}

	rec := get("map_unit_test_fixture.webp")
	if rec.Code != 200 {
		t.Fatalf("缺图应回占位 SVG: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Fatalf("占位应为 SVG: %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `width="2048" height="2048"`) {
		t.Fatalf("占位 SVG 必须带固有尺寸: %s", body)
	}
	if !strings.Contains(body, "ref/paper_maps/map_unit_test_fixture.webp") {
		t.Fatalf("占位应提示部署路径: %s", body)
	}

	// URL 片段进 XML 前转义（评审建议项）。
	rec = get(`map_<script>alert(1)>.webp`)
	if rec.Code != 200 {
		t.Fatalf("注入名应仍回占位: %d", rec.Code)
	}
	body = rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("贴图名未转义就进了 XML: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("应转义为实体: %s", body)
	}

	// 路由名约束：路径穿越、无 map_ 前缀、非 .webp 一律 404。
	for _, bad := range []string{"../secret.webp", "unit_test.webp", "map_unit_test.png", ""} {
		if rec := get(bad); rec.Code != 404 {
			t.Fatalf("非法贴图名应 404: %q -> %d", bad, rec.Code)
		}
	}
}
