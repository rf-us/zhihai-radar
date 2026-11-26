# 语义契约：Opportunity → RFERP Product / BOM

协议能通只是第一步。这个文件定义**字段怎么映射**，以及哪些东西故意不映射。

## 产品

| radar | RFERP `products` | 规则 |
|---|---|---|
| `name` | `name` | 必填，≤200 字符 |
| 采纳时填的编码 | `code` | 缺省 `OPP-{opportunity_id}`；用户可改；**不保证唯一** |
| 采纳时填的单位 | `unit` | 缺省 `台` |
| —— | `spec` | 固定 `来源：知乎选品雷达 #{id}` |
| —— | `status` | 固定 `1`（启用） |

`spec` 那一行是刻意设计的：RFERP 的产品页会直接显示它，任何人点开产品就知道来源，
不依赖 radar 是否在线。这比在 radar 里单方面记一笔"已同步"更有说服力。

## BOM

| radar `cost_model.bom_json` | RFERP `bom_items` | 规则 |
|---|---|---|
| `part_id` | `part_id` | 必须非空；空行跳过并计入 `bom_skipped` |
| `qty` | `quantity` | > 0 |
| `loss_rate` | `loss_rate` | 0–100 |
| `use_mode` | `use_mode` | 0 / 1 原样 |
| —— | `replaceable` | 固定 `0` |

**只允许选 RFERP 里已存在的零件**。手填的临时零件（`part_id` 为空）只参与毛利测算，
不进 BOM——否则 RFERP 侧会出现悬空引用，或者被迫先建零件，把一次采纳变成一串建主数据操作。

## 成本

| radar | RFERP | 规则 |
|---|---|---|
| `total_cost` | 不写入 | RFERP 没有价格字段，成本只留在 radar |
| `suggested_price` | 不写入 | 同上 |
| `margin` | 不写入 | 同上 |

这是本次集成里**唯一一处"信息不完整"**：RFERP 只拿到产品与用量，拿不到金额。
不修改 RFERP 的表结构去塞价格，是刻意的选择——那会让 radar 从"外部工具"变成"改别人库的补丁"。

## 采纳记录

radar 侧 `adoption` 表保存：`opportunity_id`、`reef_base`、`reef_database_id`、`product_id`、
`bom_created`、`bom_skipped`、`status`、`message`、`operator`、`created_at`。

RFERP 侧不留采纳记录，只通过产品的 `spec` 字段回溯。

## 幂等

- 采纳前：`GET /api/products`，若存在 `code == 目标编码` 且 `spec` 含本 opportunity id → 视为已采纳；
- 建产品超时：**不要重试**，先按上面的规则回查，确认没建成再重试；
- 本地：`adoption` 表对 `opportunity_id` 建部分唯一索引（`WHERE status='ok'`）。
