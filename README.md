# seckill-agent

事件驱动的营销决策 Agent：接收评价等业务事件，构建营销上下文，结合确定性策略与 RAG 完成营销建议，并在最终校验通过后调用 `seckill-service` 发放优惠券。

> 当前仓库是“评价触发的营销决策 Agent 核心实现 + 面向多业务事件的架构演进基线”。MySQL 事件 Inbox、Kafka 事件总线、ES 特征/案例索引和 Outcome Tracking 已在架构文档中定义，尚未全部实现为本仓库内的生产组件。

## 项目定位

本项目不让 LLM 直接决定事实、风险、金额或发券。LLM 只负责受约束的语义理解；业务正确性由事实上下文、资格校验、策略配置、幂等键、分布式锁、最终校验和下游事务共同保证。

核心链路：

```text
业务事件 -> 去重 / 幂等 -> MarketingContext -> Agent 状态推进
  -> 评价证据、统计、活动上下文、RAG -> Coupon Policy
  -> DecisionGuard / Finalizer -> seckill-service 发券接口
  -> 结果查询、审计与后续 Outcome Tracking（演进方向）
```

## 当前已实现

- Go HTTP 服务和健康检查：`GET /healthz`
- 同步 Agent 执行：`POST /api/v1/agent/run`
- Redis Streams 异步任务：提交、批量提交、查询、消费、重试、租约续期和故障恢复
- 多 Worker 并发控制，以及按评价/店铺/场景的执行隔离
- 评价证据、评价统计、活动上下文和 RAG 检索工具
- Prompt 模板、结构化 JSON 输出和 Agent 工具调用约束
- 可热加载的优惠券策略配置，以及确定性策略匹配
- Decision Guard、Finalizer、决策锁和 `grant_key` 发券幂等边界
- LLM 限流、熔断、超时传播和降级路径
- OpenTelemetry Collector + Jaeger 本地观测基础设施配置
- 评价营销场景的正确性、性能和回归测试数据

## 目标架构与边界

```text
review-service                 seckill-service
事实 / 证据 / 评价事件           活动 / 券模板 / 库存 / 事务发券
          \                         /
           -> Marketing Decision Agent
              Context -> Qualification -> Risk -> Economics
              -> Agent Intent -> RAG -> Policy -> Final Gate
              -> GrantCoupon -> Coupon/Order/Refund Outcomes
```

生产化演进重点：

1. 使用 MySQL `marketing_event_inbox`、`marketing_decision`、`grant_ledger` 和 `outcome_event` 保存事实、审计和最终状态。
2. 使用 Kafka 解耦业务事件、决策任务、发券结果和死信处理。
3. 使用 ES 保存用户/商家/商品特征和经过审核的营销案例，采用 BM25 + 向量召回 + Metadata Filter + RRF + Rerank。
4. 将短期 Session、任务租约和并发控制保留在 Redis；长期记忆分为 Audit、Aggregate、Episodic、Rule/Counterexample。
5. 发券时由 Agent Final Gate 做一次校验，再由 `seckill-service` 在短事务内完成库存、预算、权益唯一性、`grant_ledger` 和 Outbox 校验。

详细设计见：

- [Agent 执行模型](docs/agent_execution_model.md)
- [存储、并发与优惠券幂等](docs/agent_storage_and_idempotency.md)
- [评价营销评测方案](docs/review_coupon_evaluation.md)

## 目录结构

```text
.
├── configs/                     # 本地配置与优惠券策略
├── docker/                      # Redis、OTel Collector、Jaeger 本地基础设施
├── docs/                        # 执行模型、存储幂等、评测方案
├── internal/
│   ├── agent/                   # Agent 编排、策略、Guard、Finalizer
│   ├── client/                  # review-service、review-job、seckill-service 客户端
│   ├── contextx/                # 上下文拼装与 Prompt 预算
│   ├── job/                     # Redis Streams 异步任务与 Worker
│   ├── memory/                  # Session 与摘要记忆
│   ├── resilience/              # 限流、熔断、锁与重试控制
│   ├── server/http/             # HTTP API、响应和中间件
│   └── tool/                    # 工具注册、证据、统计和 RAG
├── prompts/                     # System Prompt、RAG 指引、输出 Schema
├── pkg/logx/                    # 日志上下文辅助
└── testdata/evaluation/         # 评价营销评测样例
```

## 本地运行

环境要求：Go `1.25.8`、Docker Compose，以及可选的 Redis、review-service、review-job、seckill-service 和 DeepSeek API。

启动本地 Redis 与观测组件：

```bash
docker compose -f docker/docker-compose.agent-infra.yml up -d
```

配置文件为 `configs/config.json`。LLM API Key 和 Redis 密码支持环境变量覆盖，至少建议在运行前设置：

```bash
export SECKILL_AGENT_LLM_API_KEY="your-api-key"
export SECKILL_AGENT_REDIS_PASSWORD="your-local-password"
go run .
```

Windows PowerShell：

```powershell
$env:SECKILL_AGENT_LLM_API_KEY = "your-api-key"
$env:SECKILL_AGENT_REDIS_PASSWORD = "your-local-password"
go run .
```

执行测试：

```bash
go test ./...
```

## API 示例

同步执行：

```bash
curl -X POST http://localhost:8070/api/v1/agent/run \
  -H 'Content-Type: application/json' \
  -d '{"task":"review_marketing_decision","review_id":10001,"store_id":10}'
```

异步任务提交和查询：

```bash
curl -X POST http://localhost:8070/api/v1/agent/jobs \
  -H 'Content-Type: application/json' \
  -d '{"review_id":10001,"store_id":10,"source_event_id":"review.created:10001:v1"}'

curl http://localhost:8070/api/v1/agent/jobs/{job_id}
```

## 公开仓库检查清单

- 不提交真实 API Key、生产密码、内部域名、用户隐私或业务数据。
- `configs/config.json` 和 Docker Compose 中的密码仅用于本地演示，公开前应替换为环境变量并轮换任何曾经使用过的凭据。
- 提交前确认工作区中的重构删除、新增和修改都是本项目预期内容；不要把 IDE 配置、日志、构建产物和本地数据提交进去。
- 先执行 `gofmt`、`go test ./...`，再检查 `git diff --check` 和 `git status`。

## License

当前仓库未声明开源许可证。如计划公开使用或允许他人复用，请补充 `LICENSE` 文件并选择合适的许可证。
