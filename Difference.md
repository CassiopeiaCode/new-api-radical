# 和Newapi上游的区别

1. 【已实现】模型健康度（5 分钟切片 → 小时聚合 → 成功率展示）

- 原始需求（保留）：实现维护模型健康度（健康度是一个时间的比值，每5分钟一个单位，如果该单位内只有失败的请求，那么记为失败时间片，如果有一个或多个成功请求并且（返回的byte长度大于1k或完成token大于2或实际响应模型回复大于2char），记为成功时间片，可查看不同小时时间段的模型成功率），实现后端和对应前端，，设计数据结构和新表实现良好性能。实现对非管理员隐藏可自定义模型和时间的查询（在控制台），并实现在导航栏添加新的页面（新页面所有用户即使非登录也可查看），显示所有模型最近24小时每小时的健康度。

- 口径与数据结构（后端聚合“最小单位=5分钟切片”）
  - 5 分钟切片对齐：[`model.AlignSliceStartTs()`](model/model_health_slice.go:44) 使用 `createdAt - (createdAt % 300)`（`300s` 常量见 [`modelHealthSliceSeconds`](model/model_health_slice.go:13)）。
  - “成功且满足阈值”判定：[`model.IsQualifiedSuccess()`](model/model_health_slice.go:48) 实现 `responseBytes > 1024 || completionTokens > 2 || assistantChars > 2`；事件归一化在 [`(*model.ModelHealthEvent).Normalize()`](model/model_health_slice.go:52) 内计算 `SuccessIsQualified`。
  - 新表结构：[`model.ModelHealthSlice5m`](model/model_health_slice.go:16) 映射表名 `model_health_slice_5m`（[`ModelHealthSlice5m.TableName()`](model/model_health_slice.go:29)）；主键为 `(model_name, slice_start_ts)`，并带按时间/模型索引字段（gorm tag 见 [`model.ModelHealthSlice5m`](model/model_health_slice.go:16)）。

- 写入路径与性能设计（异步队列 + UPSERT）
  - 写入入口封装：[`model.RecordModelHealthEventAsync()`](model/model_health_writer.go:36) 将事件推入内存队列（[`modelHealthEventQueueSize`](model/model_health_writer.go:11)）并由固定 worker 消费（[`modelHealthWorkerCount`](model/model_health_writer.go:12)），避免请求路径同步写库。
  - UPSERT 聚合：[`model.UpsertModelHealthSlice5m()`](model/model_health_slice.go:70) 使用 `ON CONFLICT`（gorm clause）按 `(model_name, slice_start_ts)` 做增量更新：
    - `total_requests / error_requests / success_qualified_requests` 累加（见 [`updates`](model/model_health_slice.go:102)）
    - `has_success_qualified` 采用 OR 聚合（见 [`"has_success_qualified": gorm.Expr(...)`](model/model_health_slice.go:106)）
    - `max_response_bytes / max_completion_tokens / max_assistant_chars` 用 `GREATEST` 取最大（见 [`updates`](model/model_health_slice.go:102)）
  - 说明：代码提供了“事件写入器 + 聚合表”的通用能力；调用方在“成功响应/失败响应”处构造 [`model.ModelHealthEvent`](model/model_health_slice.go:33) 并调用 [`model.RecordModelHealthEventAsync()`](model/model_health_writer.go:36) 即可把请求结果滚入 5 分钟切片统计。

- 小时聚合查询（按小时 bucket 计算 success_slices/total_slices/成功率）
  - 管理员查询单模型小时聚合：[`controller.GetModelHealthHourlyStatsAPI()`](controller/model_health.go:62)
    - 入参：`model_name` 必填；时间可用 `start_hour/end_hour` 或 `hours=ts,ts...`（解析见 [`controller.parseHourListParam()`](controller/hour_utils.go:9)，对齐校验见 [`controller.isAlignedHour()`](controller/hour_utils.go:34)）
    - 后端查询：[`model.GetModelHealthHourlyStats()`](model/model_health_query.go:42) 从 `model_health_slice_5m` 聚合到小时：
      - 小时桶表达式：[`model.hourStartExprSQL()`](model/model_health_query.go:20) 兼容 mysql/sqlite/postgres（避免整数/浮点除法差异）
      - 成功率表达式：[`model.successRateExprSQL()`](model/model_health_query.go:37) 强制 float 除法避免截断
    - 返回补齐：当某小时无数据时，API 会补 0 行，保证前端渲染稳定（补齐逻辑见 [`controller.GetModelHealthHourlyStatsAPI()`](controller/model_health.go:111)）。

- 公共页面数据源（无需登录，展示所有模型最近 24h 每小时健康度）
  - 公共 API：[`controller.GetPublicModelsHealthHourlyLast24hAPI()`](controller/model_health.go:155) 计算 `start_hour/end_hour`（对齐到整点）后调用 [`model.GetAllModelsHealthHourlyStats()`](model/model_health_query.go:75)，并按“每模型 × 24 小时”补齐缺失小时（补齐见 [`controller.GetPublicModelsHealthHourlyLast24hAPI()`](controller/model_health.go:173)）。
  - 路由与鉴权：
    - 管理员接口 `/api/model_health/hourly`：在 [`router.SetApiRouter()`](router/api-router.go:11) 中挂载并强制 [`middleware.AdminAuth()`](router/api-router.go:276)（满足“非管理员隐藏可自定义模型和时间的查询（在控制台）”）。
    - 公共接口 `/api/public/model_health/hourly_last24h`：在 [`router.SetApiRouter()`](router/api-router.go:11) 中挂载且无鉴权（满足“新页面所有用户即使非登录也可查看”所需的数据源）。
  - 缓存：公共接口带 Redis + 内存双层缓存，key/TTL 定义见 [`publicModelHealthCacheKey`](controller/model_health.go:19) 与 [`publicModelHealthCacheTTL`](controller/model_health.go:20)；读取见 [`getPublicModelHealthCache()`](controller/model_health.go:234)，写入见 [`setPublicModelHealthCache()`](controller/model_health.go:258)。

2. 【已实现】管理员豁免“用户请求限速 RPM”（在无法按标签精确解除某类限速时的兜底）

- 原始需求（保留）：若Newapi无法实现在对所有用户限速的情况下，使用标签解除对应的限速（而不是其他限速），那么添加管理员豁免用户限速RPM

- 具体实现位置（限速点与豁免点）
  - 实际生效的“用户请求限速”中间件：[`middleware.ModelRequestRateLimit()`](middleware/model-rate-limit.go:167)
  - 豁免判断：在中间件内获取当前请求用户 `id` 后，调用 [`setting.IsModelRequestRateLimitExemptUser()`](setting/rate_limit.go:67)；若命中则直接放行并打标头 `X-RateLimit-Bypass: ModelRequestRateLimit`（见 [`middleware.ModelRequestRateLimit()`](middleware/model-rate-limit.go:167) 里的豁免分支）。
  - 限速开关：[`setting.ModelRequestRateLimitEnabled`](setting/rate_limit.go:15)（在 [`middleware.ModelRequestRateLimit()`](middleware/model-rate-limit.go:167) 的请求入口实时检查，关闭则 `c.Next()`）。

- 限速策略（两套计数：总请求 + 成功请求）
  - 时间窗口：`duration := ModelRequestRateLimitDurationMinutes * 60`（见 [`setting.ModelRequestRateLimitDurationMinutes`](setting/rate_limit.go:16) 与 [`middleware.ModelRequestRateLimit()`](middleware/model-rate-limit.go:167)）。
  - 总请求数（包含失败）：`ModelRequestRateLimitCount`（见 [`setting.ModelRequestRateLimitCount`](setting/rate_limit.go:17)）；Redis 模式下通过令牌桶限制（见 [`limiter.New(...).Allow()`](middleware/model-rate-limit.go:101)）。
  - 成功请求数：`ModelRequestRateLimitSuccessCount`（见 [`setting.ModelRequestRateLimitSuccessCount`](setting/rate_limit.go:18)）；Redis 模式下用 list 记录成功请求时间戳并比较窗口（见 [`checkRedisRateLimit()`](middleware/model-rate-limit.go:25) 与成功计数 key 构造 [`successKey`](middleware/model-rate-limit.go:85)）。
  - 仅成功请求才计入成功限制：请求结束后 `c.Writer.Status() < 400` 才写入成功计数（见 [`redisRateLimitHandler()`](middleware/model-rate-limit.go:78) 与 [`memoryRateLimitHandler()`](middleware/model-rate-limit.go:132) 的“请求成功才记录”逻辑）。
  - 说明：当 `totalMaxCount == 0` 时，总请求限制跳过（见 [`checkRedisRateLimit()`](middleware/model-rate-limit.go:25) 与 [`memoryRateLimitHandler()`](middleware/model-rate-limit.go:132) 对 total 的判断），仅剩“成功请求数限制”。

- 分组覆盖（不同 group 可配置不同限速）
  - 分组读取：优先 token group，其次 user group（见 [`common.GetContextKeyString()`](middleware/model-rate-limit.go:188) 读取 [`constant.ContextKeyTokenGroup`](middleware/model-rate-limit.go:188) / [`constant.ContextKeyUserGroup`](middleware/model-rate-limit.go:190)）。
  - 分组限速配置：[`setting.GetGroupRateLimit()`](setting/rate_limit.go:85) 允许覆盖默认 `totalMaxCount/successMaxCount`（见 [`middleware.ModelRequestRateLimit()`](middleware/model-rate-limit.go:167) 内的覆盖逻辑）。

- 豁免配置的数据结构与更新方式（Root/管理员通过 option 写入）
  - 豁免列表存储：[`setting.ModelRequestRateLimitExemptUserIDs`](setting/rate_limit.go:20)（`map[int]struct{}`）。
  - 更新/解析：[`setting.UpdateModelRequestRateLimitExemptUserIDs()`](setting/rate_limit.go:55) + [`setting.ParseModelRequestRateLimitExemptUserIDs()`](setting/rate_limit.go:34) 支持逗号/空白/换行等分隔，非法 id 会报错（`invalid userId`）。
  - 配置下发：option 系统会暴露与加载 `ModelRequestRateLimit*` 相关键（见 [`model/option.go`](model/option.go:110) 写入与 [`model/option.go`](model/option.go:285) 读取开关/参数）。
3. 【已实现】最近 100 次 API 调用请求/响应缓存（含错误与上游流式原始 chunk）+ 管理员 UI 查阅

- 原始需求（保留）：实现缓存最近100次API调用的请求和返回信息到内存里（包括报错，记录客户端原始请求和上游原始响应（包括上游原始流式响应）），提供UI查阅，实现后端和对应前端，设计数据结构和新表实现良好性能

- 后端：内存环形缓存结构（容量=100，按请求 id 覆盖）
  - 单例与容量：[`service.RecentCallsCache()`](service/recent_calls_cache.go:116) 返回全局单例，默认容量 [`DefaultRecentCallsCapacity`](service/recent_calls_cache.go:22)=100。
  - 环形缓冲实现：[`type recentCallsCache`](service/recent_calls_cache.go:97) 维护 `buffer []*RecentCallRecord` + `nextID atomic.Uint64`；写入位置由 `idx := int(rec.ID % capacity)` 决定（见 [`(*recentCallsCache).put()`](service/recent_calls_cache.go:437)），天然只保留最近 N 条。
  - 记录结构：[`service.RecentCallRecord`](service/recent_calls_cache.go:80) 包含 `UserID/ChannelID/ModelName/Request/Response/Stream/Error`，覆盖“客户端原始请求 + 上游原始响应 + 上游原始流式响应 + 报错”。

- 后端：记录入口（请求开始 / 非流式响应 / 流式 chunk / 错误）
  - 请求开始（记录客户端原始请求）：[`(*recentCallsCache).BeginFromContext()`](service/recent_calls_cache.go:143)
    - 用户/渠道从 context 取：[`constant.ContextKeyUserId`](service/recent_calls_cache.go:159)、[`constant.ContextKeyChannelId`](service/recent_calls_cache.go:160)
    - headers 脱敏：[`sanitizeHeaders()`](service/recent_calls_cache.go:490) 会 mask `authorization/x-api-key/x-goog-api-key/proxy-authorization`
    - 请求 body 省略/截断：[`encodeBodyForRecord()`](service/recent_calls_cache.go:510) 对 `multipart/form-data` 直接 omit（原因 `multipart_form_data`）；文本按 [`DefaultMaxRequestBodyBytes`](service/recent_calls_cache.go:24) 截断
    - 将 record id 写入 gin context：key 为 [`RecentCallsContextKeyID`](service/recent_calls_cache.go:20)
  - Relay 主链路接入（确保每次请求都会 Begin）：在 [`controller.Relay()`](controller/relay.go:65) 中读取 requestBody 后调用 [`service.RecentCallsCache().BeginFromContext()`](controller/relay.go:208)（若之前未写入 recent_calls_id）。
  - 非流式上游响应：[`(*recentCallsCache).UpsertUpstreamResponseByContext()`](service/recent_calls_cache.go:218)，例如 OpenAI 非流式路径调用见 [`service.RecentCallsCache().UpsertUpstreamResponseByContext()`](relay/channel/openai/relay-openai.go:209)，记录 `status_code/headers/body` 并按 [`DefaultMaxResponseBodyBytes`](service/recent_calls_cache.go:25) 截断。
  - 流式上游响应（保存 raw chunk + 聚合文本）
    - 初始化 stream：[`(*recentCallsCache).EnsureStreamByContext()`](service/recent_calls_cache.go:255)（OpenAI 流式见 [`EnsureStreamByContext()`](relay/channel/openai/relay-openai.go:114)，Gemini 流式见 [`EnsureStreamByContext()`](relay/channel/gemini/relay-gemini.go:1075)）
    - 追加 raw chunk：[`(*recentCallsCache).AppendStreamChunkByContext()`](service/recent_calls_cache.go:283)，单 chunk 按 [`DefaultMaxStreamChunkBytes`](service/recent_calls_cache.go:27) 截断，总量按 [`DefaultMaxStreamTotalBytes`](service/recent_calls_cache.go:28) 限制（超限标记 `chunks_truncated`）
    - 写入聚合 assistant 文本：[`(*recentCallsCache).FinalizeStreamAggregatedTextByContext()`](service/recent_calls_cache.go:323)（OpenAI 写入见 [`FinalizeStreamAggregatedTextByContext()`](relay/channel/openai/relay-openai.go:190)，Gemini 写入见 [`FinalizeStreamAggregatedTextByContext()`](relay/channel/gemini/relay-gemini.go:1119)）
  - 错误记录：[`(*recentCallsCache).UpsertErrorByContext()`](service/recent_calls_cache.go:196)，在 [`processChannelError()`](controller/relay.go:357) 里写入（见 [`UpsertErrorByContext()`](controller/relay.go:360)），包含 `message/type/code/status`。

- 后端：管理端查询 API（debug 路由）
  - 列表：[`controller.GetRecentCalls()`](controller/debug_recent_calls.go:11) 支持 `limit` 与 `before_id`，数据来自 [`(*recentCallsCache).List()`](service/recent_calls_cache.go:383)（按 id 倒序）。
  - 单条：[`controller.GetRecentCallByID()`](controller/debug_recent_calls.go:33) 调用 [`(*recentCallsCache).Get()`](service/recent_calls_cache.go:353) 返回 request/response/stream/error 详情。
  - 路由挂载：[`router.SetApiRouter()`](router/api-router.go:11) 的 debug group 注册 `/api/debug/recent_calls` 与 `/api/debug/recent_calls/:id`（见 [`debugRoute.GET("/recent_calls"...`](router/api-router.go:54)）。

- 前端：管理员 UI 页面与入口
  - 路由：`/console/recent-calls` 懒加载 [`RecentCalls`](web/src/App.jsx:56) 并受 [`AdminRoute`](web/src/App.jsx:24) 保护（见该路由定义 [`/console/recent-calls`](web/src/App.jsx:312)）。
  - 侧边栏入口：“最近调用”菜单项 `recent_calls -> /console/recent-calls`（见 [`routerMap`](web/src/components/layout/SiderBar.jsx:33) 与 [`adminItems`](web/src/components/layout/SiderBar.jsx:151)）。
  - API 封装：[`getRecentCalls()`](web/src/services/recentCalls.js:22) 与 [`getRecentCallById()`](web/src/services/recentCalls.js:36) 请求 `/api/debug/recent_calls*`。
  - 页面实现：[`RecentCallsPage`](web/src/pages/RecentCalls/index.jsx:331) 列表（limit/before_id 翻页）+ 右侧 SideSheet 详情（请求/响应 CodeViewer + 流式 SSEViewer 回放）；403 时跳转 `/forbidden`（见 [`isAxiosError403()`](web/src/pages/RecentCalls/index.jsx:59) 与 [`query()`](web/src/pages/RecentCalls/index.jsx:341)）。

4. 【已实现】生成随机兑换码（支持前缀、数量、随机额度区间、并下载 txt 文件）

- 原始需求（保留）：实现生成随机兑换码（输入最小值和最大值，以及其他普通兑换码具有的字段，并且支持设置生成的兑换码前缀，生成随机的兑换码并提供文件下载），实现后端和对应前端

- 后端：随机 Key 生成（前缀 + 随机字符串，最大长度 32）
  - 最大长度常量：[`redemptionKeyMaxLength`](controller/redemption.go:65)=32。
  - 前缀输入：请求体字段 `key_prefix`（[`dto.CreateRedemptionRequest.KeyPrefix`](dto/redemption.go:12)），后端在 [`AddRedemption()`](controller/redemption.go:67) 里 `strings.TrimSpace()`（见 [`keyPrefix := strings.TrimSpace(req.KeyPrefix)`](controller/redemption.go:131)）。
  - 前缀长度保护：至少留 8 位随机段（[`minRandomLength`](controller/redemption.go:134)=8），若前缀过长直接返回错误（见 [`prefixLen > redemptionKeyMaxLength-minRandomLength`](controller/redemption.go:136)）。
  - 随机段长度：`randomLength := redemptionKeyMaxLength - prefixLen`（[`randomLength`](controller/redemption.go:145)），然后调用 [`common.GenerateRandomCharsKey()`](controller/redemption.go:149) 生成随机字符串并拼接成最终 key（[`key := keyPrefix + randomPart`](controller/redemption.go:159)）。

- 后端：随机额度区间（min/max）与兼容逻辑
  - DTO 字段：[`dto.CreateRedemptionRequest.RandomQuotaEnabled`](dto/redemption.go:15)、[`dto.CreateRedemptionRequest.QuotaMin`](dto/redemption.go:16)、[`dto.CreateRedemptionRequest.QuotaMax`](dto/redemption.go:17)。
  - 模式判断：[`dto.CreateRedemptionRequest.RandomQuotaMode()`](dto/redemption.go:27) 兼容两种开启方式：
    - `random_quota_enabled=true`
    - 或者同时提供 `quota_min` + `quota_max`
  - 校验逻辑：在 [`AddRedemption()`](controller/redemption.go:67) 里，随机额度模式要求 `quota_min/quota_max` 必填、>0、且 `min <= max`（见 [`req.RandomQuotaMode()`](controller/redemption.go:93) 分支）。
  - 随机取值：每个兑换码独立生成额度，使用加密随机数函数 [`cryptoRandIntInclusive()`](controller/redemption.go:281) 在 `[min,max]` 之间取整（见 [`randomQuota, err := cryptoRandIntInclusive(...)`](controller/redemption.go:164)）。

- 后端：批量生成与返回（用于前端下载）
  - 生成数量：[`dto.CreateRedemptionRequest.EffectiveCount()`](dto/redemption.go:20) 对 `count<=0` 做兼容（默认 1）；在 [`AddRedemption()`](controller/redemption.go:67) 里使用 `count := req.EffectiveCount()`（[`count`](controller/redemption.go:83)）。
  - 批量生成：循环 `count` 次构造 [`model.Redemption`](controller/redemption.go:177) 并 `Insert()`（[`cleanRedemption.Insert()`](controller/redemption.go:185)）；成功 key 追加到 `keys`（[`keys = append(keys, key)`](controller/redemption.go:195)）。
  - 返回格式：创建成功时 `data/keys` 都返回 `[]string`（见 [`"data": keys, "keys": keys`](controller/redemption.go:201)），前端可直接拿到列表用于下载 txt。

- 前端：表单字段与下载（创建成功后弹窗确认并下载）
  - 创建表单：[`EditRedemptionModal`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:60)
    - 新建时提供 `key_prefix` 输入（见 [`field='key_prefix'`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:329)）
    - 随机额度开关：`random_quota_enabled`（见 [`Form.Switch field='random_quota_enabled'`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:372)），开启后展示 `quota_min/quota_max` 两个输入（见 [`field='quota_min'`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:387) 与 [`field='quota_max'`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:415)）
  - 提交请求：新建走 [`API.post('/api/redemption/', localInputs)`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:181)，随机额度模式会写入：
    - `random_quota_enabled=true`（[`localInputs.random_quota_enabled = true`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:167)）
    - `quota_min/quota_max`（[`localInputs.quota_min`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:168)，[`localInputs.quota_max`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:169)）
  - 文件下载：后端返回 `keys`（或兼容读 `data`）后弹出确认框，并用 [`downloadTextAsFile()`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:24) 下载 `${name}.txt`（见 [`downloadTextAsFile(text, \`\${localInputs.name}.txt\`)`](web/src/components/table/redemptions/modals/EditRedemptionModal.jsx:218)）。

5. 【已实现】每渠道“模型角色映射”（将特定 role 转换为另一种 role，不是全局模型映射）
 
- 原始需求（保留）：实现模型自定义配置没有将特定role转换为另一种role的功能的话实现它。不是全局模型映射，而是每个渠道一个配置，加入渠道额外设置
 
- 配置入口（前端：渠道额外设置 JSON）
  - 渠道编辑弹窗提供字段 `model_role_mappings`（纯字符串 JSON）：[`EditChannelModal`](web/src/components/table/channels/modals/EditChannelModal.jsx:122) 的默认值见 [`originInputs.model_role_mappings`](web/src/components/table/channels/modals/EditChannelModal.jsx:131)。
  - UI 组件：在“渠道额外设置”卡片中使用 [`JSONEditor field='model_role_mappings'`](web/src/components/table/channels/modals/EditChannelModal.jsx:3142)；placeholder 里明确说明“仅作用于当前渠道”（满足“每个渠道一个配置”）。
  - 提交时写入渠道的 `setting` 字段：
    - 解析/校验：提交前用 [`verifyJSON()`](web/src/components/table/channels/modals/EditChannelModal.jsx:1252) 校验，成功后 `JSON.parse()` 存入 `channelExtraSettings.model_role_mappings`（见 [`channelExtraSettings.model_role_mappings = JSON.parse(...)`](web/src/components/table/channels/modals/EditChannelModal.jsx:1257)）。
    - 序列化：最终 `localInputs.setting = JSON.stringify(channelExtraSettings)`（见 [`localInputs.setting = JSON.stringify(channelExtraSettings)`](web/src/components/table/channels/modals/EditChannelModal.jsx:1266)），后端持久化该 JSON。
  - 编辑回填兼容：读取旧渠道时，`data.setting` 解析后会兼容 `model_role_mappings` 被存成“对象”或“JSON 字符串”两种形态（见 [`if (typeof rawMappings === 'string')`](web/src/components/table/channels/modals/EditChannelModal.jsx:572) 分支）。
 
- 后端：ChannelSettings DTO 兼容与字段承载（不是全局，随渠道走）
  - 字段定义：[`dto.ChannelSettings.ModelRoleMappings`](dto/channel_settings.go:61) 使用 [`dto.ModelRoleMappingsField`](dto/channel_settings.go:14) 承载 `model_role_mappings`。
  - 兼容三种入库形态（历史/后端/前端写法）
    - object（推荐）：`{ "gpt-4o": { "system": "developer" } }`（见注释 [`ModelRoleMappingsField supports...`](dto/channel_settings.go:8)）
    - string（双层 JSON）：`"{\"gpt-4o\":{\"system\":\"developer\"}}"`（[`UnmarshalJSON()`](dto/channel_settings.go:16) 在检测到首字符 `"` 时递归解析内部 JSON）
    - legacy flat：`{ "system": "developer" }` 会被自动提升为 wildcard `*`（见 [`"*": flat`](dto/channel_settings.go:47)），用于“对所有模型生效”的兜底。
 
- 后端：映射解析/校验（只允许 OpenAI roles；错误配置自动忽略并告警）
  - role 白名单：[`allowedOpenAIRoles`](service/model_role_mapping.go:34) 允许 `system/user/assistant/developer/tool`。
  - 解析与强校验：[`service.ParseAndValidateModelRoleMappingsJSON()`](service/model_role_mapping.go:50)
    - JSON 必须是 object：`map[modelPrefix]map[fromRole]toRole`（见 [`expected object`](service/model_role_mapping.go:62)）
    - fromRole/toRole 必须都在白名单内（见 [`IsAllowedOpenAIRole()`](service/model_role_mapping.go:99) 的校验点）。
  - 防御式读取渠道设置：[`service.GetModelRoleMappingsFromChannelSettings()`](service/model_role_mapping.go:104)
    - 从 gin context 读取当前选中渠道的 [`constant.ContextKeyChannelSetting`](service/model_role_mapping.go:110)（该 context 在选中渠道后由中间件填充）
    - 将 `setting.ModelRoleMappings` 重新 marshal 成 JSON 再调用解析函数做二次校验（见 [`ParseAndValidateModelRoleMappingsJSON(string(b))`](service/model_role_mapping.go:124)）；失败只 `logger.LogWarn` 并返回 `false`，避免错误配置影响请求。
 
- 匹配规则（按模型前缀最长匹配；支持 wildcard "*")
  - 选择策略：[`service.ResolveRoleMappingForModel()`](service/model_role_mapping.go:135)
    - 普通前缀：`strings.HasPrefix(model, prefix)`（见 [`strings.HasPrefix`](service/model_role_mapping.go:152)）
    - wildcard：prefix 为 `"*"` 时匹配任意模型但优先级最低（见 [`candidateLen = 0`](service/model_role_mapping.go:151)）
    - 最终取“匹配且前缀最长”的 roleMap（见 [`if matched && candidateLen > bestLen`](service/model_role_mapping.go:156)），保证更具体的模型规则覆盖 default/*。
 
- 应用点（关键：每次重试前先恢复原 role，再按渠道映射重写）
  - snapshot 原始 role：在 relay 解析请求后立即做快照（见 [`roleSnapshot := service.SnapshotRequestRoles(request)`](controller/relay.go:117)），覆盖两种请求结构：
    - Chat Completions：保存每条 message 的 role（见 [`SnapshotRequestRoles()`](service/model_role_mapping.go:167) 的 `MessagesRoles`）
    - Responses API：保存 `input[]` 的 role（见 [`SnapshotRequestRoles()`](service/model_role_mapping.go:181) 的 `InputRoles`）
  - 每次选中/切换渠道（含重试）后：先恢复原 role，再应用当前渠道映射（见 [`RestoreRequestRoles(...); ApplyModelRoleMappingsToRequest(...)`](controller/relay.go:192)）
    - 恢复：[`service.RestoreRequestRoles()`](service/model_role_mapping.go:201) 把 role 还原到最初客户端请求，避免“多次重试叠加映射导致 role 漂移”
    - 映射：[`service.ApplyModelRoleMappingsToRequest()`](service/model_role_mapping.go:251) 针对不同请求类型分别处理：
      - Chat Completions：[`applyToGeneralOpenAIRequest()`](service/model_role_mapping.go:271) 遍历 `messages[i].role` 并按 `roleMap[orig]` 替换（见 [`r.Messages[i].Role = target`](service/model_role_mapping.go:284)）
      - Responses API：[`applyToOpenAIResponsesRequest()`](service/model_role_mapping.go:293) 解析 `input[]` 后按 `roleMap` 替换并回写 JSON（见 [`r.Input = b`](service/model_role_mapping.go:335)）
  - 异常 role 处理：如果请求里出现非白名单 role，且未在映射表中，会通过 [`warnUnknownRoleOnce()`](service/model_role_mapping.go:338) 仅告警一次（按 `model|role` 去重），降低日志噪声。
 
6. 【已实现】强制在日志记录 IP（即使用户关闭 IP 记录开关）
 
- 原始需求（保留）：实现强制在日志记录IP，即使用户关闭IP记录
 
- 用户侧开关仍存在，但“日志写入 IP”不再受其影响
  - 用户设置字段：[`dto.UserSetting.RecordIpLog`](dto/user_settings.go:3)（json key `record_ip_log`）用于个人设置开关。
  - 保存接口：[`controller.UpdateUserSetting()`](controller/user.go:1123) 接收 [`UpdateUserSettingRequest.RecordIpLog`](controller/user.go:1109) 并写回用户 setting（见 [`settings.RecordIpLog: req.RecordIpLog`](controller/user.go:1250)）。
 
- 实际日志写入点：消费日志/错误日志均无条件写入 `c.ClientIP()`
  - 错误日志：[`model.RecordErrorLog()`](model/log.go:99) 中 `Ip` 字段直接取 [`c.ClientIP()`](model/log.go:120)（`c == nil` 才返回空串）。
  - 消费日志：[`model.RecordConsumeLog()`](model/log.go:158) 同样对 `Ip` 字段直接取 [`c.ClientIP()`](model/log.go:181)。
  - 结论：无论用户 `record_ip_log` 设为 true/false，只要请求具备 gin context，日志表 `logs.ip` 都会被写入（满足“强制记录 IP”）。
 
7. 【已实现】[`web/public/oauth-redirect.html`](web/public/oauth-redirect.html:1) 多站点 OAuth 重定向回调页（回调需先跳到该页）
 
- 原始需求（保留）：web\public\oauth-redirect.html 多站点重定向登录 回调需跳到该页否则会出错
 
- 设计目标：在“第三方 OAuth 回调域名固定/受限”的情况下，把回调落到当前站点的静态页，再安全跳回发起登录的原站点
  - 解析参数：从 querystring 读取 `code/state/error`（见 [`const code = params.get('code')`](web/public/oauth-redirect.html:148) 与 [`const finalState = params.get('state')`](web/public/oauth-redirect.html:149)）。
  - 错误兜底：若 `error` 存在，直接展示失败状态并停止跳转（见 [`if (error) { ... ui.showError(...) }`](web/public/oauth-redirect.html:153)）。
  - 必要参数校验：缺少 `code/state` 直接报错（见 [`if (!code || !finalState)`](web/public/oauth-redirect.html:160)）。
 
- “多站点”实现：把 origin 域名编码进 `state`，回调时解码后拼接跳转 URL
  - state 编码结构：`baseState|b64(originDomain)`（解码逻辑见 [`const parts = finalState.split('|')`](web/public/oauth-redirect.html:166) 与 [`const originDomain = atob(encodedDomain)`](web/public/oauth-redirect.html:178)）。
  - 构造跳转目标：使用当前协议 + 解码出的域名，拼回业务回调路径（见 [`redirectUrl = \`${protocol}//${originDomain}/oauth/linuxdo?code=${code}&state=${baseState}\``](web/public/oauth-redirect.html:184)）。
  - 无域名信息兜底：`state` 不带 `|` 时，退回“本域名”直接跳 `/oauth/linuxdo`（见 [`redirectUrl = \`/oauth/linuxdo?code=${code}&state=${finalState}\``](web/public/oauth-redirect.html:171)）。
  - 体验：延迟 800ms 让用户看到“授权成功/目标域名”（见 [`setTimeout(..., 800)`](web/public/oauth-redirect.html:192)）。
 
8. 【已实现】接入 FingerprintJS：记录用户最近 5 次去重 visitor id + ip（按 ip+visitor_id 去重）+ 管理员查询同 visitor id 用户（工作台面板“关联追踪”）
  
- 原始需求（保留）：实现接入 fingerprintjs/fingerprintjs 库，记录用户的历史5次visitor id + ip（按 ip+visitor_id 去重后的5次），并实现管理员查询相同visitor id的用户，也就是说创建一个管理员可见的在工作台的面板，面板名称4个字
  
- 前端：采集 visitor id + 1 小时节流上报
  - 依赖：前端包已引入 `@fingerprintjs/fingerprintjs`（动态加载见 [`loadFingerprintJS()`](web/src/hooks/common/useFingerprint.js:31)）。
  - 上报策略：默认 1 小时一次（[`REPORT_INTERVAL`](web/src/hooks/common/useFingerprint.js:24)），`localStorage` 记录上次上报时间（[`LAST_REPORT_KEY`](web/src/hooks/common/useFingerprint.js:27)）。
  - 上报接口：[`reportFingerprint()`](web/src/hooks/common/useFingerprint.js:53) POST `/api/fingerprint/record`（见 [`API.post('/api/fingerprint/record'...`](web/src/hooks/common/useFingerprint.js:55)）。
  - 缓存 visitor id：写入 `localStorage`（[`VISITOR_ID_KEY`](web/src/hooks/common/useFingerprint.js:28)；写入见 [`localStorage.setItem(VISITOR_ID_KEY, visitorId)`](web/src/hooks/common/useFingerprint.js:84)）。
  - Hook 用法：登录后触发一次非强制采集（见 [`useFingerprint(isLoggedIn)`](web/src/hooks/common/useFingerprint.js:102)）。
 
- 后端：记录到表 `user_fingerprints`，同用户最多保留 5 个不同 `ip + visitor_id` 组合（按 ip+visitor_id 去重）
  - 写入入口：[`controller.RecordFingerprint()`](controller/fingerprint.go:16) 读取 `visitor_id`（[`RecordFingerprintRequest`](controller/fingerprint.go:11)），并取 `User-Agent` 与 [`c.ClientIP()`](controller/fingerprint.go:35) 一起入库。
  - 数据表：[`model.UserFingerprint`](model/user_fingerprint.go:10) 映射表名 `user_fingerprints`（[`TableName()`](model/user_fingerprint.go:20)）。
  - 去重逻辑：后端使用 upsert，按 `(user_id, visitor_id, ip)` 组合去重；命中则更新 `user_agent/updated_at`（实现见 [`model.RecordFingerprint()`](model/user_fingerprint.go:25)）。
  - 保留 5 条：超过 5 个 `ip + visitor_id` 组合记录时，删除第 6 条之后的旧记录（见 [`model.RecordFingerprint()`](model/user_fingerprint.go:25) 的 `Offset(5)` 清理逻辑）。
  - MySQL 迁移（必须）：为保证并发下去重正确，需要添加复合唯一索引（否则可能产生重复行，导致“保留 5 条”失真）：
    ```sql
    ALTER TABLE user_fingerprints
      ADD UNIQUE KEY ux_user_fingerprints_user_visitor_ip (user_id, visitor_id, ip);
    ```
 
- 管理员查询：重复指纹列表 + 点进查看关联用户
  - 路由挂载（管理员）：在 [`router.SetApiRouter()`](router/api-router.go:11) 的 admin fingerprint group 下提供：
    - 列表：[`adminFingerprintRoute.GET("/duplicates"...`](router/api-router.go:296) → [`controller.GetDuplicateVisitorIds()`](controller/fingerprint.go:125)
    - 查用户：[`adminFingerprintRoute.GET("/users"...`](router/api-router.go:296) → [`controller.FindUsersByVisitorId()`](controller/fingerprint.go:100)
  - “重复”口径：visitor_id + ip 组合下 `COUNT(DISTINCT user_id) > 1` 才算重复（见 [`model.GetDuplicateVisitorIds()`](model/user_fingerprint.go:205) 的 `GROUP BY visitor_id, ip HAVING ...`）。
  - UI 面板（4 个字）：页面标题为 [`title={t('关联追踪')}`](web/src/pages/Fingerprint/index.jsx:384)，并提供“重复指纹/全部记录”两 tab（见 [`<Tabs ...>`](web/src/pages/Fingerprint/index.jsx:398)）。
 
9. 【已实现】活跃任务槽追踪系统（全局 1000 / 单用户 50，上下文哈希匹配，LRU 淘汰）+ 600s 高活跃扫描入库 + 24h token 消耗查询
 
- 原始需求（保留）：实现维护每个用户100个槽，储存在内存中，每个槽是一次哈希和时间的记录，在8,64,512...长度的多个哈希结果（每个仅保存6位）。每当遇到请求时，都先计算哈希并和槽进行比较，如果能继承，那么继承并覆盖，否则LRU占用新槽。接下来实现一个查询页面，展示用户在30秒内的活跃任务数（即槽数）feat: 活跃任务槽追踪系统 - 全局1000槽/单用户50槽上限，多级哈希匹配，LRU淘汰策略，管理员查询页面。添加功能：实现每600秒扫描一次，如果发现活跃任务数在5（600s）以上的记录到新表 可查询。实现记录的用户可点击查看其24小时内消耗的不同模型的多少token
 
- 核心数据结构：内存槽 + 多级哈希前缀 + LRU
  - 全局/单用户上限：[`MaxGlobalSlots`](model/active_task_slot.go:24)=1000、[`MaxUserSlots`](model/active_task_slot.go:26)=50（与需求文案一致）。
  - 活跃窗口：默认 30s（[`ActiveWindowSeconds`](model/active_task_slot.go:28)），排名 API 可调 `window`（见 [`controller.GetActiveTaskRankAPI()`](controller/active_task.go:22)）。
  - 哈希层级：`8,64,512,4096,32768,131072`（见 [`hashLevels`](model/active_task_slot.go:35)），每级保存 `HashPrefixLen=16` 字节前缀（见 [`HashPrefixLen`](model/active_task_slot.go:30) 与 [`TaskSlot.HashPrefix`](model/active_task_slot.go:44)）。
  - 计算方式：增量 sha256（见 [`computeHashPrefixes()`](model/active_task_slot.go:79)；只写入新增片段见 [`h.Write([]byte(data[prevEnd:end]))`](model/active_task_slot.go:95)）。
  - 匹配策略：只比较“当前请求最高级往下 MatchLevelCount=2 个层级”（见 [`MatchLevelCount`](model/active_task_slot.go:41) 与 [`matchHashPrefix()`](model/active_task_slot.go:114)）。
  - LRU 淘汰：每次命中/复用都会移动到 LRU 末尾（见 [`moveToLRUEnd()`](model/active_task_slot.go:244)）。
 
- 记录入口：从请求上下文抽取“可识别对话连续性”的数据
  - 只统计 chat 类请求：路径命中 `/chat/completions`、`/v1/completions`、`/v1/responses`、`/v1/messages`、Gemini `generateContent`（见 [`isChatRequest := ...`](model/active_task_slot.go:447)）。
  - 优先使用缓存过的请求 body：读取 gin context 的 [`common.KeyRequestBody`](common/gin.go:20)（见 [`gc.Get("key_request_body")`](model/active_task_slot.go:458)），若为空则退回 `modelName`（见 [`if data == "" { data = modelName }`](model/active_task_slot.go:464)）。
  - 写入动作：[`RecordActiveTaskSlot()`](model/active_task_slot.go:428) → `manager.RecordTask(...)`（见 [`manager.RecordTask(userID, username, data)`](model/active_task_slot.go:469)）。
  - 实际接入点：每次记录消费/错误日志时都会顺带记录活跃槽（见 [`RecordActiveTaskSlot(...)`](model/log.go:139) 与 [`RecordActiveTaskSlot(...)`](model/log.go:214)）。
 
- 管理员 API + 前端查询页
  - 路由（管理员）：[`activeTaskRoute`](router/api-router.go:306) 挂载：
    - 实时排名：[`GET /api/active_task/rank`](controller/active_task.go:17)
    - 统计信息：[`GET /api/active_task/stats`](controller/active_task.go:53)
    - 高活跃历史：[`GET /api/active_task/history`](controller/active_task.go:61)
    - 24h token 消耗：[`GET /api/active_task/user_token_usage`](controller/active_task.go:94)
  - 前端页面：[`ActiveTaskRankPage`](web/src/pages/ActiveTaskRank/index.jsx:42)
    - 实时 tab：轮询刷新 5s（见 [`setInterval(..., 5000)`](web/src/pages/ActiveTaskRank/index.jsx:196)），调用 `/api/active_task/rank`（见 [`API.get('/api/active_task/rank'`](web/src/pages/ActiveTaskRank/index.jsx:88)）。
    - 历史 tab：查询 `/api/active_task/history`（见 [`API.get('/api/active_task/history'`](web/src/pages/ActiveTaskRank/index.jsx:129)）。
    - token 弹窗：点击“Token消耗”调用 `/api/active_task/user_token_usage`（见 [`API.get('/api/active_task/user_token_usage'`](web/src/pages/ActiveTaskRank/index.jsx:170)）。
 
- 600s 高活跃扫描 → 新表落库
  - 扫描周期：[`HighActiveTaskScanInterval`](model/active_task_slot.go:331)=600s；阈值：[`HighActiveTaskThreshold`](model/active_task_slot.go:333)=5；窗口：[`HighActiveTaskWindowSeconds`](model/active_task_slot.go:335)=600s。
  - 启动扫描器：[`StartHighActiveTaskScanner()`](model/active_task_slot.go:365) 使用 ticker 定时调用 [`scanAndSaveHighActiveUsers()`](model/active_task_slot.go:376)。
  - 新表：[`model.HighActiveTaskRecord`](model/active_task_slot.go:339) 映射 `high_active_task_records`（[`TableName()`](model/active_task_slot.go:349)）。
  - 过滤管理员：扫描保存时会跳过管理员（见 [`if IsAdmin(u.UserID) { continue }`](model/active_task_slot.go:388)）。
 
- “查看该用户 24h 不同模型 token 消耗”
  - 后端：[`controller.GetUserTokenUsage24hAPI()`](controller/active_task.go:98) 计算 `startTimestamp = now - 24*60*60`（见 [`startTimestamp := now - 24*60*60`](controller/active_task.go:107)），调用 [`model.GetUserTokenUsageByModel()`](model/log.go:436)。
  - 查询来源：优先走 `quota_data` 小时聚合表（见注释 [`优先使用quota_data表（数据看板统计表）`](model/log.go:437) 与实际查询 [`DB.Table("quota_data")`](model/log.go:442)），返回 `{model_name,total_tokens,request_count}`（见 [`ModelTokenUsage`](model/log.go:428)）。
 
10. 【已实现】合并上游签到更新 + 增加“是否开启随机额度”开关
 
- 原始需求（保留）：合并上游的签到更新。添加是否开启随机额度功能
 
- 配置结构：checkin_setting 同时支持固定额度与随机额度模式
  - 后端配置结构：[`operation_setting.CheckinSetting`](setting/operation_setting/checkin_setting.go:7) 包含：
    - `enabled`（[`Enabled`](setting/operation_setting/checkin_setting.go:8)）
    - `min_quota/max_quota`（随机模式区间，见 [`MinQuota`](setting/operation_setting/checkin_setting.go:9) / [`MaxQuota`](setting/operation_setting/checkin_setting.go:10)）
    - `fixed_quota`（固定模式额度，见 [`FixedQuota`](setting/operation_setting/checkin_setting.go:11)）
    - `random_mode`（是否随机额度，见 [`RandomMode`](setting/operation_setting/checkin_setting.go:12)），默认 true（见 [`RandomMode: true`](setting/operation_setting/checkin_setting.go:21)）。
  - 配置注册：通过 [`config.GlobalConfig.Register("checkin_setting"...`](setting/operation_setting/checkin_setting.go:26) 纳入 option 系统。
 
- 用户侧接口：查询状态 / 执行签到
  - 路由：用户登录后 `/api/user/checkin` GET/POST（见 [`selfRoute.GET("/checkin"...`](router/api-router.go:104)）。
  - 状态接口返回随机/固定模式信息：[`controller.GetCheckinStatus()`](controller/checkin.go:16) 返回 `fixed_quota/random_mode/min_quota/max_quota`（见 [`"random_mode": setting.RandomMode`](controller/checkin.go:35)）。
  - 执行签到：[`controller.DoCheckin()`](controller/checkin.go:49) 调用 [`model.UserCheckin(userId)`](controller/checkin.go:58) 并写系统日志（见 [`model.RecordLog(... "用户签到，获得额度" ...)`](controller/checkin.go:67)）。
 
- 管理端 UI：系统设置里的“随机额度模式”开关
  - 页面：[`SettingsCheckin`](web/src/pages/Setting/Operation/SettingsCheckin.jsx:32)。
  - 开关字段：[`Form.Switch field={'checkin_setting.random_mode'}`](web/src/pages/Setting/Operation/SettingsCheckin.jsx:127)，并在“启用签到功能”关闭时禁用（见 [`disabled={!inputs['checkin_setting.enabled']}`](web/src/pages/Setting/Operation/SettingsCheckin.jsx:135)）。
  - 字段联动：随机模式下启用 `min_quota/max_quota`，固定模式下启用 `fixed_quota`（见 [`disabled ... isRandomMode`](web/src/pages/Setting/Operation/SettingsCheckin.jsx:152) 与 [`disabled ... !isRandomMode`](web/src/pages/Setting/Operation/SettingsCheckin.jsx:167)）。