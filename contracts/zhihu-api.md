# 知乎开放平台接口契约（radar 依赖面）

> 来源：官方 Skill 包 `references/http-api.md`（核验时间 2026-07-16）
> 基线：`https://developer.zhihu.com`
> 本文件只记录 radar 实际调用的四个接口，不是完整 API 文档。

## 鉴权

```
Authorization: Bearer <Access Secret>
X-Request-Timestamp: <秒级 Unix 时间戳>
Content-Type: application/json
```

Access Secret 在 `https://developer.zhihu.com/profile` 申请，**只从环境变量读取**
（`ZHIHU_ACCESS_SECRET`），不落库、不进日志、不下发前端。

额度（邀测期，同一账号下所有 Secret 共享）：

| 能力 | 每日额度 |
|---|---|
| 知乎搜索 | 5,000 |
| 全网搜索 | 5,000 |
| 知乎热榜 | **100** |
| 知乎直答 | **100** |

## 1. 知乎搜索

```
GET /api/v1/content/zhihu_search?Query=<关键词>&Count=<1-10>
```

```json
{"Code":0,"Message":"success","Data":{"HasMore":false,"Items":[{
  "Title":"便携咖啡机值得买吗","ContentText":"清洗麻烦","Url":"https://www.zhihu.com/q/1",
  "CommentCount":89,"VoteUpCount":312,"AuthorityLevel":"3","EditTime":1757606400}]}}
```

注意：`AuthorityLevel` 是**字符串**，radar 折成 0–4 使用；`EditTime` 是秒级时间戳。

## 2. 全网搜索

```
GET /api/v1/content/global_search?Query=&Count=<1-20>&Filter=&SearchDB=<all|realtime|static>
```

`Filter` 支持 `host=="example.com"`、`publish_time>=1778494631`，`AND`/`OR` 必须大写，
作为 URL Query 参数时需要编码。响应结构与知乎搜索一致。

## 3. 知乎热榜

```
GET /api/v1/content/hot_list
```

```json
{"Code":0,"Message":"success","Data":{"Total":1,"Items":[
  {"Title":"…","Url":"https://www.zhihu.com/hot/1","ThumbnailUrl":"","Summary":"…"}]}}
```

热榜条目**没有点赞/评论/时间**，因此只参与 D3 的"是否命中"，不参与 D1 加权。

## 4. 知乎直答（大模型）

```
POST /v1/chat/completions
{"model":"zhida-fast-1p5","stream":false,"messages":[{"role":"user","content":"…"}]}
```

模型档位：`zhida-fast-1p5`（默认）/ `zhida-thinking-1p5` / `zhida-agent`。
响应对齐 OpenAI：`choices[0].message.content`。支持 `stream: true`，radar 暂不用流式。

## 错误处理

| 情况 | radar 行为 |
|---|---|
| HTTP 401/403 | `ErrUnauthorized`，提示检查 Access Secret |
| HTTP 429 | `ErrRateLimited` |
| `Code=30001` | `ErrRateLimited` |
| `Code=30002` | `ErrQuotaExhausted` |
| 5xx / 网络错误 | `ErrUnavailable` |

**不自动重试**：配额类错误重试只会更快烧完当天额度。单路失败不阻断整次分析，
分数按可用维度归一化并在 `degraded_reason` 里说明。
