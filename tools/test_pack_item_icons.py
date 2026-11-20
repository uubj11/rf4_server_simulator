"""图标包导出/打包脚本的回归测试。"""
import tempfile
import unittest
import zipfile
from pathlib import Path

from pack_item_icons import FIXED_ZIP_TIME, build_archive


class PackIconTests(unittest.TestCase):
    def test_archive_is_deterministic_and_skips_empty_files(self):
        with tempfile.TemporaryDirectory(prefix="rf4-icons-") as tmp:
            root = Path(tmp)
            item_dir = root / "ref" / "item_icons"
            fish_dir = root / "ref" / "fish_icons"
            item_dir.mkdir(parents=True)
            fish_dir.mkdir(parents=True)
            (item_dir / "bait.webp").write_bytes(b"webp-data")
            (item_dir / "empty.webp").write_bytes(b"")
            (fish_dir / "cod.png").write_bytes(b"png-data")
            (fish_dir / "ignore.txt").write_text("ignored", encoding="utf-8")
            out = root / "ref" / "icons.zip"

            total, skipped = build_archive(root, out)
            first = out.read_bytes()
            total2, skipped2 = build_archive(root, out)

            self.assertEqual((total, skipped), (2, 1))
            self.assertEqual((total2, skipped2), (2, 1))
            self.assertEqual(first, out.read_bytes(), "相同输入必须生成字节一致的图标包")
            with zipfile.ZipFile(out) as zf:
                self.assertEqual(zf.namelist(), ["item_icons/bait.webp", "fish_icons/cod.png"])
                for info in zf.infolist():
                    self.assertEqual(info.date_time, FIXED_ZIP_TIME)
                    self.assertEqual(info.compress_type, zipfile.ZIP_STORED)

    def test_oversized_icon_is_rejected(self):
        with tempfile.TemporaryDirectory(prefix="rf4-icons-") as tmp:
            root = Path(tmp)
            item_dir = root / "ref" / "item_icons"
            item_dir.mkdir(parents=True)
            with (item_dir / "huge.webp").open("wb") as fh:
                fh.truncate((4 << 20) + 1)
            with self.assertRaises(ValueError):
                build_archive(root, root / "ref" / "icons.zip")
    def test_empty_input_does_not_replace_existing_archive(self):
        with tempfile.TemporaryDirectory(prefix="rf4-icons-") as tmp:
            root = Path(tmp)
            out = root / "ref" / "icons.zip"
            out.parent.mkdir(parents=True)
            out.write_bytes(b"keep-me")
            with self.assertRaises(ValueError):
                build_archive(root, out)
            self.assertEqual(out.read_bytes(), b"keep-me")


if __name__ == "__main__":
    unittest.main()
