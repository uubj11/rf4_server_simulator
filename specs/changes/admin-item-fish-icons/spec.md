# 管理后台物品与鱼种图标

Status: done

## Problem Statement

管理后台的物品、鱼种和搜索弹窗只有文字，管理员核对装备、鱼饵和鱼种时需要在游戏内反复确认。直接把上万张图标散文件或 `go:embed` 资源入库会拖慢 clone/PR/CI 并扩大二进制体积；直接提交官方客户端素材又违反 `CONTRIBUTING.md` 的素材红线。

## Solution

后台列表与搜索弹窗按需显示图标。图标素材由离线工具从管理员本地客户端导出，打包为部署目录的可选 `ref/icons.zip`；仓库只提交提取、打包、运行时加载代码。图标包缺失、损坏或未部署时，图片隐藏，列表与管理功能不受影响。

## User Stories

1. 作为管理员，我希望在物品和鱼种列表中看到图标，以便快速核对装备和鱼。
2. 作为发布维护者，我希望本地生成的图标包能随 Preview/Release staging 一起装配，同时不让官方素材进入 Git 历史。
3. 边界：缺包、坏包、零字节图、超限条目或路径穿越条目都不能导致管理端崩溃；替换图标包后重启即可生效。

## Implementation Decisions

- 模块：`internal/admin/item_icons.go` 提供 `/icons/{items|fish}/{name}` 静态图片路由；`internal/admin/api.go` 在 SPA FileServer 前注册。
- 素材：`tools/extract_item_icons.py` 导出到 `.gitignore` 覆盖的 `ref/`；`tools/pack_item_icons.py` 生成确定性 zip；`tools/stage_preview.py` 仅在本地包存在时装配。
- 安全边界：只接受 `items`/`fish` 白名单目录和 `.webp`/`.png`；拒绝路径分隔符、目录项和非法扩展名；压缩包上限 128 MiB、最多 20000 个条目、单图上限 4 MiB；读取后关闭文件句柄，按 Server 实例缓存。
- 降级：散文件优先；无散文件时读取 zip；缺包或坏包返回 404；前端 `onerror` 隐藏图片，不改变列表布局。
- 许可与兼容：不提交客户端素材、导出原图或含素材截图；图标是可选部署资源，不改变协议、数据库、经济或鉴权链路。

## Testing Decisions

- Go 单元：`internal/admin/item_icons_test.go` 覆盖安全条目过滤、路径穿越、坏包、包/条目/单图大小限制、Content-Type、缓存头、缺包 404 和并发读取。
- UI：`internal/admin/item_icons_ui_test.cjs` 覆盖物品与鱼种 URL 编码、图标成功显示和 404 时隐藏。
- 脚本：`tools/test_pack_item_icons.py` 覆盖确定性 zip、空文件跳过和失败不覆盖旧包；`tools/test_stage_preview.py` 覆盖可选图标包装配与大小上限。
- 门禁：`gofmt`、`go test ./... -count=1`、`go vet ./...`、`go build ./...`、`go run ./tools/doccheck`、Python 脚本测试、相关 Playwright 测试。

## Out of Scope

- 不把官方客户端图标作为仓库资源分发，也不自动下载客户端素材。
- 不新增图标管理 API、数据库表或权限模型；图标仍是无需登录的静态图片资源。
- 不改变物品/鱼种业务字段，不处理图标视觉设计或素材版权授权。

## Further Notes

- `[事实]` `.gitignore` 的 `/ref/` 已排除客户端逆向资料；`docs/admin-console.md` 记录部署步骤。
- `[事实]` 图标包不进入 `go:embed`，避免 launcher 体积随素材增长。
- `[推断]` 图标包格式与 UI 的 item_id/fish_key 命名约定一致；运行测试验证实际 URL 与响应。
