# 对话与验证卡交接

对话 API 复用 `radar_session` HttpOnly 登录 Cookie。一个登录会话持有一条当前对话，服务端在内存保存最近 60 条消息、六个槽位、修订号和当前分析。浏览器只在 `localStorage` 保存 `session_id`，同一登录会话内刷新可恢复。退出、登录过期或 Radar 进程重启后需重新开始，已创建的机会仍保存在 SQLite。

## 对话 HTTP

所有路由前缀为 `/api/dialogue/sessions`，结果是当前对话快照。

| 方法与路径 | 请求体和行为 |
| --- | --- |
| `POST` 到前缀 | 无请求体。创建或取回当前对话，重复请求返回同一条。 |
| `GET /{id}` | 读取消息、槽位和真实进度。 |
| `POST /{id}/turns` | `{"revision":2,"text":"北美"}`。抽取槽位，返回追问、就绪或 `refused`。 |
| `PUT /{id}/slots/{slot}` | `{"revision":3,"value":"eu"}`。值为空字符串时清空。方向支持自填，顾虑多选以逗号分隔，其余使用 `questions[].options` 中的值。 |
| `POST /{id}/analyze` | `{"revision":4}`。只有 `ready` 可启动，返回 `202`，用 `GET /{id}` 观察结果。 |
| `POST /{id}/cancel` | `{"revision":5}`。取消本次取数并保留已填槽位。若机会已提交，返回对应的 `done` 快照。 |
| `POST /{id}/next` | `{"revision":6}`。当前对话 `done` 后开启新对话，返回 `201`。 |

快照字段包括 `session_id`、`revision`、`status`、`slots`、`questions` 和 `messages`。四项必填是品类方向、目标市场、目标零售价、供应链基础。主要顾虑与验证深度可选，后者默认先看结论。`messages[].kind` 为 `user`、`question`、`ready` 或 `refused`，`changed` 提供可直接展示的槽位回执。

未登录返回 `401`，对话不存在或属于另一个登录会话返回 `404`。过时的修订号或状态不允许写入时返回 `409`，客户端重新读取快照后再操作。实时知乎分析沿用分析配额限制，`429` 带 `Retry-After`。

## 分析和机会

快照的 `status` 依次为 `collecting`、`ready`、`analyzing`、`done`。失败或取消后回到 `ready`，原槽位仍可编辑和重试。`analysis.status` 独立标记 `running`、`failed`、`canceled` 或 `succeeded`，`analysis.mode` 标明 `zhihu`、`fixture` 或 `unavailable`。

`analysis.sources` 的键为 `zhihu_search`、`global_search`、`hot` 和 `answer`。每路独立报告 `pending`、`running`、`succeeded`、`failed` 或 `canceled`，失败带 `reason`。直答等知乎搜索结束后启动，以便复用检索摘要。两路搜索均失败时返回可重试错误。部分来源失败仍可生成机会，留存失败原因。成功只表示该调用完成，证据是否命中以返回数据为准。

机会、证据和分析上下文在同一个 SQLite 事务提交。取消先于提交生效时不会生成机会。提交先完成时返回真实机会 ID。对同一轮分析的并发启动只有一个会被接受。

## Issue #10 可用的数据

`done` 带 `opportunity_id`，前端进入 `/opportunity/{id}`。通过现有 `GET /api/opportunity?id={id}` 读 `opportunity`、`evidence`、`reasons`、`degraded` 和 `degraded_reason`。

- `opportunity.scores.cost` 为 `null` 时，成本仍待测算。
- `opportunity.analysis` 随结果持久化，旧机会可能没有此字段。新分析包含 `mode`、`sources`，对话产生的机会另有 `plan`。`plan` 保留市场、供应链、目标价中值 `target_usd` / `target_cny`、顾虑、深度、证据展示建议 `evidence_max` 和条件报告节 `sections`。分析保存统一最多 30 条原文证据供后续界面选取。
- 验证卡展示 API 返回的全部证据。`opportunity.created_at` 是整批证据与机会共同落库时的时间，可用于标记采集批次。`evidence[].published_at` 是原文发布时间。
- 同一 `id` 用于 `/opportunity/{id}/supply`、`/opportunity/{id}/cost` 和 `/opportunity/{id}/report`。报告页默认从 `plan.sections` 读取条件节，并传给 PDF 接口的 `sections` 参数。成本页默认带入 `plan.target_cny`，保存后从机会详情读回重算分数。
