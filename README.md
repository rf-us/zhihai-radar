# 知海选品雷达 / Zhihai Radar

把知乎上的真实讨论，变成可验证的出海选品结论，并一键落到 RFERP 的产品与 BOM 里。

> 参赛作品：知乎黑客松 S3 · 湾区共创季 · 赛道 03「AI × 出海」
> 本仓库为独立实现，**不包含也不修改** [RFERP](https://github.com/Wu-Wh0/RFERP) 的任何源码；
> 两者通过公开 HTTP API 对接，契约见 [`contracts/reef-api.md`](contracts/reef-api.md)。

## 它解决什么

出海卖家选品靠拍脑袋：看什么火就上什么，没有证据、没有成本测算、结论落不了地。

这个工具做三件事：

1. **挖**：从知乎搜索、热榜、全网讨论里提取真实需求与抱怨；
2. **算**：五维模型（需求热度 / 痛点强度 / 趋势信号 / 竞争供给 / 成本可行性）打分，每个分数都能点开原文；
3. **落**：把候选产品采纳为 RFERP 的产品与 BOM，直接进入原有的批次追溯流程。

## 架构

```
浏览器 ──同源──▶ radar :8090 ──HTTP + Bearer──▶ reef :8080
                  │                              （MySQL / SQLite）
                  ├─ web（go:embed）
                  ├─ /api/analyze  知乎四接口 + 五维评分
                  ├─ /api/adopt    采纳 Saga（建产品 + 建 BOM + 回滚）
                  └─ radar.db      自有 SQLite：候选 / 证据 / 成本 / 采纳记录
```

三条纪律：

- 浏览器**永不直连** reef（避开 CORS，reef token 不落前端）；
- radar **不写** reef 的数据库，所有写入走 reef 的 API；
- reef token **只存进程内存**，不落盘、不入日志。

## 快速开始

```bash
# 1. 起一个 RFERP（另开一个终端；用一次性数据目录，别碰业务库）
mkdir -p tmp && printf '{"storage":"sqlite","sqlitePath":"./tmp/reef.db"}' > config.json
MES_DATA_DIR=./tmp ./rferp-server -addr :8080

# 2. 起 radar
export REEF_BASE=http://127.0.0.1:8080
go run ./cmd/radar
# → http://127.0.0.1:8090
```

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `RADAR_ADDR` | `:8090` | 监听地址 |
| `RADAR_DATA_DIR` | `./data` | SQLite 与缓存目录 |
| `REEF_BASE` | `http://127.0.0.1:8080` | RFERP 地址 |
| `REEF_DEVICE_ID` | 空 | RFERP 2.x 局域网版要求的设备指纹；对接 ≤ 4d73ae7 的旧版时留空 |
| `ZHIHU_ACCESS_SECRET` | 空 | 知乎 Access Secret，**只从环境变量读** |
| `RADAR_SOURCE` | `zhihu` | `zhihu` / `fixture`（离线演示） |

## 仓库结构

```
cmd/radar/           入口
internal/reef/       RFERP 客户端 + 采纳 Saga + 契约测试
internal/domain/     五维模型（纯函数，无 IO）
internal/source/     知乎适配（待建：四接口 / 缓存 / 配额 / fixture）
internal/store/      SQLite 仓储与版本化迁移
internal/httpapi/    HTTP 面
web/                 go:embed 的静态资源
contracts/           对外可评审的接口契约
docs/                ADR 与合规披露
```

## 开发状态

- [x] 仓库骨架、接口契约、RFERP 客户端与契约测试
- [ ] 知乎适配层（搜索 / 全网 / 热榜 / 直答）
- [ ] 五维评分与验证卡
- [ ] 成本测算
- [ ] 采纳 Saga 的 HTTP 面
- [ ] Web 界面

## 合规

见 [`docs/compliance.md`](docs/compliance.md)。要点：本仓库代码为团队原创；RFERP 仅作为外部依赖被调用；
不抓取、不存储知乎用户隐私，仅使用公开内容摘要与原文链接。

## License

MIT
