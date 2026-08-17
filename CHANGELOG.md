# 更新日志

## 未发布

### 新增

- 渠道详情增加 Sub2API 余额查询，服务端使用所选渠道密钥请求 `/v1/usage`，兼容额度、钱包及旧版余额字段。
- 分组增加 Auto 智能路由策略，根据渠道/模型的成功率、延迟和样本量动态排序，并提供采样窗口与延迟权重设置。

### 改进

- Relay 日志正文默认限制为 2 MiB，并限制 attempts 决策记录，降低超大请求、图片响应和异常重试导致的数据库膨胀风险。
- Gemini 出站转换过滤不受支持的 JSON Schema 关键字，提高 Claude Code 等工具调用场景的兼容性。

### 文档

- README 增加 2026 年 7～8 月阶段性成果，集中说明 Codex/Responses、智能路由、渠道诊断、国内网络适配、日志安全和移动端改进。

## v0.9.9 - 2026-08-16

### 新增

- 网络与服务设置增加模型价格代理地址，可通过大陆可访问的镜像或反代接口更新 `models.dev` 价格数据。

## v0.9.8 - 2026-08-16

### 新增

- 渠道详情增加图片生成测试，可指定模型与密钥，预览 URL/Base64 图片，并在浏览器保存提示词。
- 增加 `/v1/codex/responses` 与 `/backend-api/codex/responses` 的 HTTP/SSE、WebSocket 兼容入口。
- 增加可选的 Codex 429→503 重试兼容设置，默认关闭，日志仍保留真实上游状态。
- 渠道创建、编辑、模型获取和备份导入增加 Base URL 合法性检查。

### 修复

- 修正 OpenAI Responses 在 `length`、`content_filter` 和不完整工具调用场景下的终止状态与输出项状态。
- 上游未返回 usage 但提供明确 `finish_reason` 时，在 `[DONE]` 阶段补发正确的 Responses 终止事件。
- 下游 Responses WebSocket 现在会把 Codex Session、Thread 和 Turn-State 请求头传入上游连接池隔离键。
- 跨协议转换前去重重复的工具结果，保留同一工具调用最后一次返回内容。
