#!/usr/bin/env python3
"""从 RF4 客户端导出管理后台使用的物品与鱼种图标。

官方客户端素材不入仓库（见 CONTRIBUTING「素材红线」），本脚本把图标导出到
部署目录的 ref/ 下，由 internal/admin/item_icons.go 运行时加载：

    ref/item_icons/<item_id>.webp   128×128 透明，物品列表/邮件附件用
    ref/fish_icons/<fish_key>.png   128×64  透明，鱼种列表/加鱼弹窗用

用法（需要 UnityPy 与 Pillow）：
    pip install UnityPy Pillow
    python tools/extract_item_icons.py --client-data '<客户端目录>/rf4_x64_Data'

筛选口径：
  * 名称形如 <fish_key>_m|_s|_b 且 fish_key 存在于
    internal/gameserver/assets/fish_ecology_defs.json → 鱼种图标
  * 其余正方形纹理，仅当纹理名小写后存在于当前静态目录的 item_catalog → 物品图标
"""

import argparse
import json
import os
import re
import sqlite3
import sys

try:
    from PIL import Image
except ImportError:
    sys.exit("缺少 Pillow：pip install Pillow")

try:
    import UnityPy
except ImportError:
    sys.exit("缺少 UnityPy：pip install UnityPy")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FISH_DEFS = os.path.join(ROOT, "internal", "gameserver", "assets", "fish_ecology_defs.json")
ITEM_DB = os.path.join(ROOT, "server_data", "static.sqlite3")
ITEM_DIR = os.path.join(ROOT, "ref", "item_icons")
FISH_DIR = os.path.join(ROOT, "ref", "fish_icons")
FISH_SUFFIXES = ("_m", "_s", "_b")
INVALID_FILENAME = re.compile(r'[<>:"/\\|?*\x00-\x1f]')


def load_fish_keys() -> set[str]:
    with open(FISH_DEFS, encoding="utf-8") as fh:
        defs = json.load(fh)
    return {d["fish_key"] for d in defs if d.get("fish_key")}


def load_item_ids() -> set[str]:
    with sqlite3.connect(ITEM_DB) as db:
        return {row[0].lower() for row in db.execute("SELECT item_id FROM item_catalog") if row[0]}


def safe_stem(name: str):
    name = (name or "").strip()
    if not name or name in {".", ".."} or INVALID_FILENAME.search(name):
        return None
    return name


def fish_icon_name(name: str, keys: set[str]):
    for suffix in FISH_SUFFIXES:
        if name.endswith(suffix):
            base = name[: -len(suffix)]
            if base in keys:
                return base
    return None


def save_image(img: Image.Image, out: str, fmt: str, **options) -> bool:
    tmp = out + ".tmp"
    try:
        img.save(tmp, fmt, **options)
        if os.path.getsize(tmp) == 0:
            return False
        os.replace(tmp, out)
        return True
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)


def save_item(img: Image.Image, name: str) -> bool:
    stem = safe_stem(name)
    if stem is None:
        return False
    out = os.path.join(ITEM_DIR, stem.lower() + ".webp")
    img = img.convert("RGBA").resize((128, 128), Image.LANCZOS)
    return save_image(img, out, "WEBP", quality=82, method=3)


def save_fish(img: Image.Image, key: str) -> bool:
    # 鱼图源尺寸通常为 256×128，等比缩到 128×64；点号在文件名里换成下划线。
    stem = safe_stem(key.lower().replace(".", "_"))
    if stem is None:
        return False
    out = os.path.join(FISH_DIR, stem + ".png")
    img = img.convert("RGBA").resize((128, 64), Image.LANCZOS)
    return save_image(img, out, "PNG", optimize=True)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("data_dir", nargs="?", help="客户端 rf4_x64_Data 目录")
    ap.add_argument("--client-data", dest="client_data", help="同 data_dir；推荐使用显式参数")
    ap.add_argument("--limit", type=int, default=0, help="仅导出前 N 张，用于试跑")
    args = ap.parse_args()
    data_dir = args.client_data or args.data_dir
    if not data_dir:
        ap.error("需要 --client-data <客户端目录>/rf4_x64_Data")
    if args.limit < 0:
        ap.error("--limit 不能为负数")

    os.makedirs(ITEM_DIR, exist_ok=True)
    os.makedirs(FISH_DIR, exist_ok=True)
    fish_keys = load_fish_keys()
    item_ids = load_item_ids()
    env = UnityPy.load(data_dir)

    n_item = n_fish = 0
    for obj in env.objects:
        if obj.type.name != "Texture2D":
            continue
        try:
            data = obj.read()
            name = data.m_Name or ""
            img = data.image
        except Exception:
            continue
        if img is None or img.width < 32 or img.height < 32:
            continue
        key = fish_icon_name(name, fish_keys)
        if key:
            n_fish += save_fish(img, key)
        elif img.width == img.height:
            stem = safe_stem(name)
            if stem and stem.lower() in item_ids:
                n_item += save_item(img, name)
        if args.limit and n_item + n_fish >= args.limit:
            break

    print(f"物品图标 {n_item} 张 -> {ITEM_DIR}")
    print(f"鱼种图标 {n_fish} 张 -> {FISH_DIR}")


if __name__ == "__main__":
    main()
