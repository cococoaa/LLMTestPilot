# LLMTestPilot Go 化改造方案

**LLMTestPilot — LLM Agent 驱动的浏览器自动化测试平台**

> 基于 LLM Agent 构建「用例生成 → AI 执行 → 代码沉淀 → 失败自愈」全链路闭环的自动化测试平台，核心是 Agent 决策引擎、长期记忆与多模型适配层。

**技术栈：** Python、FastAPI、LangChain / LangGraph、Playwright、Mem0、Chroma、Elasticsearch、CLIP、DeepSeek / Qwen3-VL 等 30+ LLM

**核心职责：**

- **Agent 多步决策引擎**：基于 LangChain 设计「感知(DOM/截图)→决策→执行→记忆→反馈」五段闭环主循环，以思考链决策树 + 五级优先级在动态页面上自主执行多步操作（最大 30 步），单任务决策延迟 < 2s。
- **多模型统一适配层**：封装统一装配函数路由 30+ 国产/海外模型，实现三类接入路径（静态鉴权代理 / API_KEY 直连 / iChat 网关 HMAC-SHA256 实时签名），重写底层 `_generate/_stream` 实现每次请求前刷新鉴权头；对非函数调用模型自动切换 raw 模式并做 `<think>` 剥离与 JSON 容错解析，模型切换对业务无侵入。
- **跨任务长期记忆系统**：基于 Mem0 + Chroma 设计五维命名空间隔离（project/platform/workflow/environment/doctype）与 6 类记忆分类，实现「三级重要性 × 三档保留周期（365/90/7 天）」的动态升降级与分层召回（精确匹配 → 扩展召回去重），多项目记忆零污染、探索效率提升约 50%。
- **多模态断言修复 Agent**：基于 LangGraph 编排「拉日志 → 取截图 → 多模态分析 → 结构化归因」流水线，结合 CLIP 向量化 + ES kNN 检索历史相似案例，将断言失败根因定位从人工 30 分钟缩短至 2 分钟。
- **图片知识库与服务容错**：设计双服务架构（业务检索 37000 / CLIP 编码 37001）隔离 GPU 编码负载，实现 remote→local 兜底容错，基于 ES `dense_vector` + cosine kNN 做语义检索（延迟 < 200ms）。

如果想让这个项目本身就有 Go 成分，**必须是真写的**，建议二选一：

1. 用 **Go 重写模型网关/鉴权层**（就是 iChat HMAC 签名 + 30+ 模型路由那一层），天然适合 Go 的并发和中间件，工作量小、价值点清晰。
2. 加一个 **Go 写的 MCP Server / CLI**，把 Agent 的执行能力暴露成 MCP 工具——这正好踩中「Agent 应用开发」当下最热的 MCP 方向。

> 目标：在不重蹈「全量重写」覆辙的前提下，让 LLMTestPilot 带上真实、可面试的 Go 成分，匹配「Go 后端 + Agent 应用开发」方向。

## 一、结论先行：混合架构，而不是全量 Go 重写

| 方案 | 结论 | 理由 |
| --- | --- | --- |
| 整个项目 Python → Go 重写 | ❌ 不建议 | LangChain/LangGraph（Agent 编排）、Mem0（记忆）、CLIP（多模态）在 Go 生态没有一等公民等价物，重写等于重造轮子，投入产出极差 |
| **Go 网关 + Python Agent 核心** | ✅ 采用 | 把「天然适合 Go」的模型网关/鉴权层用 Go 实现，Agent 决策、记忆、多模态仍留在 Python 生态，各取所长 |

这不是妥协，而是业界真实做法：**LLM 生态在 Python 侧，而「网关、鉴权、并发转发、服务化」是 Go 的主场**。这个分工本身就是面试里能讲的判断力。

## 二、目标架构

```
                    ┌─────────────────────────────────────────────┐
                    │  Vue 3 前端（测试任务配置、报告查看）          │
                    └──────────────────────┬──────────────────────┘
                                           │ OpenAI 兼容协议
                    ┌──────────────────────▼──────────────────────┐
                    │        Go：llm-gateway（本方案已实现）        │
                    │  /v1/models   /v1/chat/completions           │
                    │  · 30+ 模型路由（配置驱动，零代码切换）         │
                    │  · 三类鉴权：adams 静态头 / polaris 直连 /     │
                    │    ichat HMAC-SHA256 实时签名                 │
                    │  · SSE 流式透传、请求体大小限制、统一错误       │
                    └───┬──────────────┬──────────────┬────────────┘
                        │ adams        │ polaris      │ ichat(HMAC)
                        ▼              ▼              ▼
                    国产模型代理    DeepSeek 直连    海外模型网关
                                   (Claude/Gemini/GPT)

                    ┌─────────────────────────────────────────────┐
                    │  Python：Agent 核心（保持不变）               │
                    │  LangChain 决策引擎 / LangGraph 断言修复 /     │
                    │  Mem0 记忆 / CLIP 图片知识库 / Playwright     │
                    └─────────────────────────────────────────────┘
```

Python Agent 核心不再直接调各模型 SDK，而是**统一调用 Go 网关的 `/v1/chat/completions`**，实现「模型切换对业务代码无侵入」——这一步就是原 `get_model()` 想要达到的效果，只是下沉到了 Go 网关。

## 三、各模块语言归属

| 模块 | 语言 | 说明 |
| --- | --- | --- |
| 模型网关 / 鉴权层 | **Go** | 本方案已实现，适合 Go 的并发与中间件 |
| Agent 决策引擎 | Python | LangChain 生态 |
| 多模态断言修复 | Python | LangGraph + 视觉模型 |
| 长期记忆系统 | Python | Mem0 生态 |
| CLIP 图片知识库 | Python（可拆独立服务） | GPU 推理生态 |
| 前端 | Vue 3 | 不受影响 |

## 四、已实现：Go 模型网关（llm-gateway）

### 4.1 与原 Python 实现的映射

| 原 Python | Go 实现 | 说明 |
| --- | --- | --- |
| `get_model()` 巨型装配函数 | `router.go` 的 `GetModel()` | 模型别名 → 上游模型 + 能力标记 |
| `IChatOpenAI._generate/_stream` | `providers.go` 的 `Complete()` | 统一转发入口 |
| iChat `_update_headers` HMAC 签名 | `signIChat()` | 每次请求刷新签名头 |
| adams 静态鉴权头 | `signProxy()` | 静态头注入 |
| polaris API_KEY 直传 | `signDirect()` | Bearer 直传 |
| 30+ 模型路由 | `config.json` 的 `models` 表 | 配置驱动，零代码切换 |
| `ModelConfig` 三元组 | `ModelConfig` 结构体 | model / use_vision / use_tool_calling |

### 4.2 目录结构

```
llm-gateway/
├── go.mod         # module llmgateway，零第三方依赖（仅标准库）
├── main.go        # HTTP 服务、/v1/models、/v1/chat/completions、SSE 透传
├── config.go      # 配置结构与加载
├── model.go       # OpenAI 兼容类型
├── router.go      # GetModel 路由（get_model 的 Go 版）
├── providers.go   # Provider 接口 + 三类鉴权实现
├── config.json    # 模型与 Provider 配置
└── .gitignore
```

### 4.3 接口契约

- `GET /healthz` → 健康检查
- `GET /v1/models` → 列出模型别名及能力（use_vision / use_tool_calling）
- `POST /v1/chat/completions` → OpenAI 兼容，支持 `stream` 的 SSE 透传

### 4.4 三类鉴权方式

| 类型 | 鉴权方式 | 适用 |
| --- | --- | --- |
| `proxy`（adams） | 静态鉴权头注入 | 国产模型代理 |
| `direct`（polaris） | `Authorization: Bearer <API_KEY>` | DeepSeek 等直连 |
| `ichat` | HMAC-SHA256 实时签名：`sign = HMAC(secret, method\npath\ntimestamp\nsha256(body))` | 海外模型网关 |

> 说明：iChat 的具体签名头字段名以实际网关文档为准，`signIChat()` 已把签名逻辑独立成函数，改动点只有一处。

### 4.5 已验证

- `go build ./...` 通过，`go vet ./...` 通过，零第三方依赖。
- 运行时验证：`/healthz`、`/v1/models`、未知模型返回 404 均正常。

## 五、后续实施步骤（分阶段，均为真实可交付）

1. **阶段一（已做）**：Go 模型网关 + 三类鉴权 + 路由。
2. **阶段二**：把 Python Agent 核心改为调用 Go 网关，替换原来的 `get_model()` 直连逻辑，验证「一键切换模型」。
3. **阶段三**：网关补中间件——请求日志、限流、超时、模型级配额、Prometheus 指标。
4. **阶段四（可选）**：加 Go 写的 MCP Server，把 Agent 能力暴露成 MCP 工具，踩中「Agent 应用开发」最热的 MCP 方向。
5. **阶段五（可选）**：CLIP 图片知识库的检索服务（ES 读写、kNN 查询）用 Go 重写，保留 Python 只做 GPU 编码。

## 六、简历 / 面试如何表述

**简历 bullet（可粘贴）：**

> 在 LLM Agent 自动化测试平台中，用 Go 从零实现**多模型统一网关**：以配置驱动路由 30+ 国产/海外模型，抽象三类接入方式（静态鉴权代理 / API_KEY 直连 / iChat 网关 HMAC-SHA256 实时签名），提供 OpenAI 兼容的 `/v1/chat/completions` 接口并支持 SSE 流式透传，实现业务侧零侵入切换模型。

**面试一句话：**

> Agent 决策、记忆、多模态这些留在 Python 生态（LangChain/Mem0/CLIP），但把「模型网关 + 鉴权 + 并发转发」这类服务化底座用 Go 单独实现——这正是「Go 后端 + Agent 应用开发」的典型分工。

## 七、一句话总结

**不硬把 Python 换成 Go，而是在 Agent 系统里找到真正属于 Go 的那一层（网关/鉴权/服务化）并完整实现**——既保住了 Agent 生态，又给出了可信的 Go 后端证据，且每一条都能在代码里被面试官看到。
