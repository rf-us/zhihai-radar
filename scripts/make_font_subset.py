"""生成 PDF 用的中文字体子集（Noto Sans SC，静态 400 字重）。

用法（在仓库根目录，Git Bash）：
    PYTHONUTF8=1 python scripts/make_font_subset.py

输入字体默认取 C:\\Windows\\Fonts\\NotoSansSC-VF.ttf，可用 --source 覆盖。
产物写入 assets/font/NotoSansSC-Regular.ttf 并入库；CI 不跑本脚本，
评委环境不需要 Python/fonttools。

字符集 = GB2312 一级汉字（3755 字，覆盖"常用 3500 字"要求）
       + fixtures/ 与 docs/ 里出现的全部字符（真实语料回归）
       + ASCII 与常用标点/货币符号。
"""

from __future__ import annotations

import argparse
import glob
import os
import sys
from pathlib import Path

try:
    from fontTools import subset
    from fontTools.ttLib import TTFont
    from fontTools.varLib import instancer
except ImportError as exc:  # pragma: no cover
    sys.exit("需要 fonttools：python -m pip install fonttools brotli\n" + str(exc))


DEFAULT_SOURCE = r"C:\Windows\Fonts\NotoSansSC-VF.ttf"
DEFAULT_OUTPUT = "internal/report/assets/font/NotoSansSC-Regular.ttf"
MAX_BYTES = 3 * 1024 * 1024


def gb2312_level1() -> set[str]:
    """GB2312 一级汉字区 0xB0A1–0xD7F9，共 3755 字。"""
    chars: set[str] = set()
    for high in range(0xB0, 0xD8):
        for low in range(0xA1, 0xFF):
            try:
                chars.add(bytes([high, low]).decode("gb2312"))
            except UnicodeDecodeError:
                continue
    return chars


def corpus_chars(root: Path) -> set[str]:
    """收集 fixtures/ 与 docs/ 里所有非 ASCII 字符，作为真实语料回归。"""
    chars: set[str] = set()
    patterns = ["fixtures/**/*.json", "docs/**/*.md", "contracts/**/*.md"]
    for pattern in patterns:
        for path in root.glob(pattern):
            try:
                chars.update(path.read_text(encoding="utf-8"))
            except UnicodeDecodeError:
                continue
    return chars


def base_chars() -> set[str]:
    """ASCII 可见字符 + 报告常用的中文标点与货币符号。"""
    chars = {chr(c) for c in range(0x20, 0x7F)}
    chars.update("，。！？、；：（）《》「」『』【】—…·￥¥$€£%°±×÷≤≥→←①②③④⑤⑥⑦⑧⑨⑩⚠□")
    chars.update("－—‑–~〜·•※§¶†‡")
    return chars


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--source", default=DEFAULT_SOURCE)
    ap.add_argument("--output", default=DEFAULT_OUTPUT)
    args = ap.parse_args()

    root = Path(__file__).resolve().parent.parent
    source = Path(args.source)
    if not source.exists():
        sys.exit(f"字体源文件不存在：{source}")
    output = root / args.output
    output.parent.mkdir(parents=True, exist_ok=True)

    charset = gb2312_level1() | corpus_chars(root) | base_chars()
    # fontTools subset 需要 unicode codepoint 集合。
    unicodes = {ord(c) for c in charset if c.isprintable()}
    print(f"字符集大小：{len(unicodes)} 个 codepoint")

    print(f"读取可变字体：{source}")
    font = TTFont(source, lazy=False)
    if "fvar" in font:
        axes = {a.axisTag: a for a in font["fvar"].axes}
        if "wght" not in axes:
            sys.exit("可变字体没有 wght 轴，无法实例化 400 字重")
        print("实例化 wght=400 静态字体…")
        font = instancer.instantiateVariableFont(font, {"wght": 400}, inplace=False)

    options = subset.Options()
    options.layout_features = ["*"]  # 保留连字等 OpenType 特性
    options.name_IDs = ["*"]
    options.notdef_outline = True
    options.recalc_bounds = True
    subsetter = subset.Subsetter(options=options)
    subsetter.populate(unicodes=unicodes)
    subsetter.subset(font)

    tmp = output.with_suffix(".tmp.ttf")
    font.save(tmp)
    size = tmp.stat().st_size
    print(f"子集字体大小：{size / 1024 / 1024:.2f} MB")
    if size > MAX_BYTES:
        tmp.unlink(missing_ok=True)
        sys.exit(f"字体超过 3MB 上限（{size} > {MAX_BYTES}），需缩小字符集")
    os.replace(tmp, output)
    print(f"已写入：{output}")

    # 同步输出"允许字符"清单：Go 渲染器用它做缺字兜底（子集外字符替换 □ 并计数）。
    # 从保存后的字体读 cmap 才是真实覆盖，避免实例化/子集过程丢字导致误判。
    saved = TTFont(output, lazy=True)
    covered: set[int] = set()
    for table in saved["cmap"].tables:
        covered.update(table.cmap.keys())
    saved.close()
    allow = output.parent / "subset-chars.txt"
    allow.write_text("".join(chr(cp) for cp in sorted(covered) if 0x20 <= cp <= 0x10FFFF), encoding="utf-8")
    print(f"已写入：{allow}（{len(covered)} 个 codepoint）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
