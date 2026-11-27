# ADR-0003：PDF 渲染选型与中文字体

- 状态：已接受
- 日期：2026-11-27
- 决策人：后端线

## 背景

报告是评委唯一能带走的东西，必须**纯 Go 生成**：评委机器不一定有 Chrome，
也不能要求先装 wkhtmltopdf 或 Python。中文报告需要嵌入中文字体，且安装包
体积受控。

## 决策

1. 渲染库用 `github.com/signintech/gopdf` v0.38.6。
2. 中文字体用 Noto Sans SC，可变字体实例化为静态 400 字重后做子集，
   产物入库在 `internal/report/assets/font/`，随二进制 `go:embed`。
3. 子集脚本 `scripts/make_font_subset.py` 只在开发机运行；CI 与评委环境
   不需要 Python/fonttools。
4. 缺字兜底：子集外字符替换为 `□` 并计数，报告末尾输出告警。

## 被否方案

| 方案 | 否决理由 |
|---|---|
| Chrome/Edge 打印 PDF | 评委机器不可控，不能假设装了浏览器 |
| wkhtmltopdf | 额外系统依赖，安装包变复杂 |
| HTML → PDF 的 Node 工具（puppeteer） | 引入 Node 运行时，违背纯 Go 目标 |
| 不子集化直接嵌入 Noto Sans SC | 字体 10MB+，安装包超预算 |
| 在 CI 里跑 fonttools | 评委/贡献者环境需要 Python 依赖，不可靠 |

## 字体子集细节

- 输入：`C:\Windows\Fonts\NotoSansSC-VF.ttf`（TrueType 可变字体）。
- 步骤：fontTools `instancer.instantiateVariableFont(wght=400)` → `subset`
  （GB2312 一级 3755 字 + fixtures/docs 语料 + ASCII/标点/货币符号）。
- 产物：`NotoSansSC-Regular.ttf`（约 1.16MB，远低于 3MB 上限）与
  `subset-chars.txt`（覆盖字符清单，供 Go 缺字检测）。
- 注意：字体文件与 `subset-chars.txt` 必须来自同一次脚本运行，
  否则缺字检测会出现误判。

## gopdf 使用注意（实测踩坑）

`pdf.Start()` **不隐式创建首页**。必须先 `pdf.AddPage()` 再写任何文本，
否则 `Cell` / `MultiCell` / `Text` 全部 panic（nil content，地址 0x8）。
本项目的 `runner.addPage()` 统一处理，调用方不要绕过。

子集字体路径下 `Cell(nil, ...)` 会 panic，必须传有尺寸的 `*gopdf.Rect`
（见 `runner.cell`）。

## 可复现性

渲染器不读时钟。`Meta.GeneratedAt` 由调用方注入；同一 `Document` 两次渲染
字节一致（有单测守住）。

## 后果

- 安装包体积增加约 1.2MB（字体），可接受。
- 子集覆盖常用 3500 字；生僻字会被替换成 □ 并计数告警，不静默出错。
- 中文断行由 gopdf 的 `MultiCell` 按字符宽度处理，中文无空格场景正常。
