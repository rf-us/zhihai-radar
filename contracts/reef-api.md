# RFERP 接口契约（radar 依赖面）

> 基线：`Wu-Wh0/RFERP` @ `4d73ae7`（2026-10-21）
> 本文件是 radar 与 RFERP 之间的**唯一约定**。radar 不读 RFERP 的数据库、不复用其源码。

## 0. 总则

- 所有请求与响应均为 `application/json; charset=utf-8`。
- 鉴权：`Authorization: Bearer <token>`，token 来自 `POST /api/login`。
- 错误统一为 `{"error": "..."}`，HTTP 状态码表达类别。
- **会话有效期：绝对 24h / 空闲 4h。** 到期后返回 401，radar 必须重新登录。
- RFERP 不设置 CORS 头，因此**只能由 radar 服务端调用**，浏览器不得直连。

## 1. 连接探测

```
GET /api/v1/serverinfo          （免登录）
```

```json
{
  "appVersion": "2.3.0",
  "apiVersion": 1,
  "schemaVersion": 13,
  "databaseId": "db-7f3c...",
  "storage": "sqlite"
}
```

注意字段是 **camelCase**，且 `apiVersion` 是**整数**，不是字符串。

用途：
1. 连接页的"测试连接"；
2. 版本兼容判断（`apiVersion != 1` 时告警但放行）；
3. 采纳记录里保存 `databaseId`，避免连错实例。

## 2. 登录

```
POST /api/login
{"username":"admin","password":"..."}
```

```json
{"token":"...", "user":{"id":1,"username":"admin","display_name":"管理员","role":"admin"}}
```

`role` 取值：`admin` / `warehouse` / `production` / `viewer`。

## 3. 当前用户

```
GET /api/me                      （需登录）
```

用于采纳前确认角色：**建产品、建 BOM、删产品都要求 `admin`**。

## 4. 零件列表

```
GET /api/parts                   （需 Catalog.ListParts，四角色可读）
```

```json
[{"id":3,"code":"P-001","name":"不锈钢滤网","spec":"120目","unit":"个",
  "part_type":"五金","stock_qty":120,"warn_qty":20,"status":1,"version":1,
  "supplier":"某供应商"}]
```

**空列表可能返回 `null` 而不是 `[]`**（Go 的 nil slice）。radar 必须容忍。

## 5. 产品列表

```
GET /api/products                （需 Catalog.ListProducts）
```

同上的 `null` 问题。用于两件事：采纳去重、建产品超时后的不确定态核对。

## 6. 建产品

```
POST /api/products               （需 Catalog.CreateProduct，仅 admin）
```

请求体**只允许**这七个字段：

```json
{"code":"OPP-7","name":"便携咖啡机","spec":"来源：知乎选品雷达 #7","unit":"台","status":1}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `code` | string | 产品编码。**允许重复**（它是系列号），去重由 radar 负责 |
| `name` | string | 产品名 |
| `spec` | string? | 规格。radar 用它写溯源信息 |
| `unit` | string | 单位 |
| `status` | int? | `1` 启用 / `2` 停用，缺省 1 |
| `id` | int64 | 仅更新时用 |
| `version` | int | 仅更新时用（乐观锁） |

> ⚠️ 服务端使用 `json.Decoder.DisallowUnknownFields()`。**多传任何字段（包括 `operator`、
> `created_at`、`source`）都会返回 `400 invalid body: json: unknown field "xxx"`。**

响应为创建后的完整产品对象，含 `id` 与 `version`。

## 7. 删产品（补偿用）

```
DELETE /api/products?id=42       （需 Catalog.DeleteProduct，仅 admin）
```

BOM 随外键 `ON DELETE CASCADE` 一并删除。用于采纳失败时的回滚。
若产品已被批次引用，删除会被拒绝——此时应转人工处理。

## 8. 建 BOM

```
POST /api/bom                    （需 Catalog.AddBOMItem，仅 admin）
{"product_id":42,"part_id":3,"quantity":2,"loss_rate":3,"use_mode":0,"replaceable":0}
```

| 字段 | 说明 |
|---|---|
| `quantity` | 单台用量；`use_mode=1` 时表示"每 M 台用 1 个"的 M |
| `loss_rate` | 损耗率百分比，0–100 |
| `use_mode` | `0` 每台用 N 个 / `1` 每 M 台用 1 个 |
| `replaceable` | 是否可替换，radar 固定传 0 |

BOM 是**逐条插入**，没有批量接口，因此需要 Saga 补偿。

## 9. 查 BOM

```
GET /api/bom?product_id=42       （需 Catalog.GetBOMByProduct）
```

用于"已同步到 RFERP"面板回读。

## 10. 产品详情

```
GET /api/products/get?id=42      （需 Catalog.GetProduct）
```

不存在时返回 `404 {"error":"product not found"}`。

## 11. 批次列表（可选）

```
GET /api/batches                 （需 Production.ListBatches）
```

P1：用于判断采纳的产品是否已经投产。

## 附：错误码与 radar 的处置

| 状态 | 含义 | radar 处置 |
|---|---|---|
| 400 | 业务校验失败 | 把 `error` 原文展示给用户（RFERP 的中文提示比自编的更有用） |
| 401 | 会话失效 | 丢弃内存 token，提示重新登录 |
| 403 | 角色不足 | 采纳预检阶段就应拦住，提示"需要管理员账号" |
| 404 | 对象不存在 | 刷新回读面板 |
| 5xx | RFERP 内部错误 | 不自动重试写操作；读操作可重试一次 |

## 附：实测记录（2026-11-26）

在 `Wu-Wh0/RFERP@4d73ae7` 源码构建的 `cmd/server`（SQLite）上逐条跑过，不是照文档抄的：

| 步骤 | 结果 |
|---|---|
| `GET /api/v1/serverinfo` | `{"appVersion":"dev","apiVersion":1,"schemaVersion":13,"databaseId":"f481…","storage":"sqlite"}` |
| `POST /api/bootstrap-admin` | 200，返回 `{"id":1,...,"role":"admin"}` |
| `POST /api/login` | 200，token 为 48 字符 |
| `GET /api/me` | 200，`role=admin` |
| `POST /api/parts` | 200；响应里 `version=0`、时间戳为零值，需重新拉列表 |
| `POST /api/products` | 200，`spec` 原样保留 |
| `POST /api/products` 多传 `operator` | **400** `invalid body: json: unknown field "operator"` |
| `POST /api/bom` | 200；`GET /api/bom?product_id=` 返回带 `part_code/part_name` |
| `DELETE /api/products?id=` | 200 `{"status":"ok"}` |
| 删除后 `GET /api/products` | **`null`**（不是 `[]`） |

三个"只有跑起来才敢确信"的点，全部命中：

1. `serverinfo` 是 **camelCase**，且 `apiVersion` 是**整数**；
2. `POST /api/products` 的 `DisallowUnknownFields` 会拒绝任何多余字段；
3. 零行列表返回 **`null`**，客户端必须兜住，否则前端 `.map` 白屏。

复现方式：

```bash
REEF_BASE=http://127.0.0.1:8080 REEF_USER=admin REEF_PASS=*** \
  go test ./internal/reef -tags=live -v
```

---

## 附：2.1.1 的破坏性变更（2026-11-27 实测）

上游 `rf-us/RFERP@92d516d`（v2.1.1）引入了局域网授权与设备准入，**旧契约不再够用**：

| 变更 | 影响 |
|---|---|
| 登录与**每个**已认证请求都要带 `X-RFERP-Device-ID` | 不带走 `400 device_missing`；登录后再去掉走 `401` |
| 会话与设备绑定 | 同一 token 换一个设备标识即失效 |
| 未导入授权时只有**主机**指纹可登录 | 其他设备返回 `403 license_missing`（"当前仅主机可登录"） |
| 登录响应新增 `runtime` 对象 | `licensed / max_clients / used_clients / policy_message` |
| schema 13 → **20** | 旧库启动时自动迁移（会先备份） |

### 设备指纹怎么来

服务端启动时会把主机指纹写进 `devices` 表（`kind=host`），值即 `SHA-256(MachineGuid + 系统盘卷序列号 + CPU 标识)`。
radar 与服务端同机部署时，把它填进 `REEF_DEVICE_ID` 即可：

```bash
python -c "import sqlite3;print(sqlite3.connect('data/rferp.db').execute(\"select device_id_hash from devices where kind='host'\").fetchone()[0])"
```

### 对雷达的影响

- 客户端必须支持设备头（已实现：`reef.Client.WithDevice`）；
- **radar 必须与 RFERP 同机部署**，或在 RFERP 侧导入授权并登记为客户端设备；
  云端 radar 连本地 RFERP 的模式在 2.1.1 上不可行；
- 若对接的是 ≤ `4d73ae7` 的旧版，`REEF_DEVICE_ID` 留空即可，行为与旧契约一致。
