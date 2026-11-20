#!/usr/bin/env python3
"""把本地导出的图标素材打包成部署目录使用的 ref/icons.zip。

管理后台从 ref/icons.zip 读取图标（见 internal/admin/item_icons.go）。
官方素材不入仓库；ref/ 已由 .gitignore 排除，发布时由
tools/stage_preview.py 在本地存在该压缩包时一并装配。

    ref/icons.zip
      item_icons/<item_id>.webp   128×128
      fish_icons/<fish_key>.png   128×64

用法：
    python tools/pack_item_icons.py
"""

import os
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "ref" / "icons.zip"
SOURCES = (
    ("item_icons", ".webp"),
    ("fish_icons", ".png"),
)
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)
MAX_ENTRIES = 20000
MAX_FILE_BYTES = 4 << 20


def build_archive(root: Path, out: Path) -> tuple[int, int]:
    """生成可重复的图标包，返回 (条目数, 跳过的空文件数)。"""
    out.parent.mkdir(parents=True, exist_ok=True)
    tmp = out.with_name(out.name + ".tmp")
    total = skipped = 0
    try:
        tmp.unlink(missing_ok=True)
        with zipfile.ZipFile(tmp, "w", compression=zipfile.ZIP_STORED, strict_timestamps=True) as zf:
            for sub, ext in SOURCES:
                src = root / "ref" / sub
                if not src.is_dir():
                    continue
                paths = sorted(
                    (p for p in src.iterdir() if p.is_file() and not p.is_symlink() and p.suffix.lower() == ext),
                    key=lambda p: p.name,
                )
                for src_path in paths:
                    size = src_path.stat().st_size
                    if size == 0:
                        skipped += 1
                        continue
                    if size > MAX_FILE_BYTES:
                        raise ValueError(f"图标超过单图大小上限: {src_path.name}")
                    if total >= MAX_ENTRIES:
                        raise ValueError("图标条目超过上限")
                    info = zipfile.ZipInfo(f"{sub}/{src_path.name}", date_time=FIXED_ZIP_TIME)
                    info.compress_type = zipfile.ZIP_STORED
                    info.create_system = 3
                    info.external_attr = 0o100644 << 16
                    zf.writestr(info, src_path.read_bytes())
                    total += 1
        if total == 0:
            raise ValueError("没有可打包的素材，请先运行 tools/extract_item_icons.py")
        os.replace(tmp, out)
        return total, skipped
    finally:
        tmp.unlink(missing_ok=True)


def main() -> None:
    try:
        total, skipped = build_archive(ROOT, OUT)
    except ValueError as exc:
        sys.exit(str(exc))
    print(f"完成: {OUT} ({OUT.stat().st_size / 1024 / 1024:.1f} MB, {total} 个条目)")
    if skipped:
        print(f"跳过空文件: {skipped} 个")


if __name__ == "__main__":
    main()