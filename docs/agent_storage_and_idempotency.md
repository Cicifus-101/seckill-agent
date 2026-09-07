# Agent Storage, Concurrency, and Coupon Idempotency

## Goal

The coupon agent keeps the LLM path flexible, but makes the engineering path deterministic:

- PostgreSQL stores long-term decisions, RAG chunks, job traces, and audit evidence.
- Redis stores short-term session context and running job status.
- Goroutines process independent reviews concurrently with bounded concurrency.
- Coupon granting is idempotent across agent retries, API retries, and duplicate review events.

## Storage Boundary

### Long-Term PostgreSQL

Use PostgreSQL for data that must survive process restart and support audit:

- `rag.review_chunks`: review summaries, aspects, and historical evidence.
- `rag.review_decisions`: final agent decisions and LLM output.
- `agent_jobs` or equivalent review-job table: async job status, retry count, error reason, session scope.
- `coupon_grant_events`: optional outbox/audit table for calls made to `seckill-service`.

`review_decisions` remains the source of truth for the current decision and its audit revisions. One store/review pair has one current decision; a higher evidence version may create a new revision before a coupon is granted.

Recommended deterministic decision key:

```text
decision:store:{store_id}:review:{review_id}
```

### Short-Term Redis

Use Redis only for hot state:

- Recent tool steps for one session.
- Compressed intermediate context.
- Running async job state.
- Lightweight dedupe locks.

Recommended keys:

```text
agent:session:{store_id}:{activity_id}:{window_id}
agent:job:{job_id}
seckill-agent:lock:decision:store:{store_id}:review:{review_id}
review:store:{store_id}:review:{review_id}
```

Default TTL can be 2 hours for session state and 10-30 minutes for running job locks.

## Session Partition

Because seckill has multiple activities, do not keep one global session.

Preferred session ID:

```text
review-coupon:store-{store_id}:activity-{activity_id}:window-{window_id}:review-{review_id}
```

Two practical modes:

1. Activity scoped: one activity has moderate traffic. Partition by `store_id + activity_id`.
2. Count-window scoped: one activity has high traffic. Partition by `store_id + activity_id + batch-N`, for example every 20-50 reviews.

The session stores short-term context only. Historical similar cases still come from PostgreSQL/RAG.

## Concurrent Execution

The API can remain synchronous for debugging, but production traffic should support async mode:

1. Request enters agent API.
2. API creates a job id and writes `PENDING`.
3. A goroutine worker picks the job with bounded concurrency.
4. Independent tools run with timeouts.
5. Final decision is validated, stored, and optionally triggers coupon grant.
6. Client polls job status or receives callback.

The job queue uses a Redis Stream consumer group. New jobs are appended with
`XADD`, workers consume with `XREADGROUP`, successful terminal processing is
confirmed with `XACK`, and stale pending messages are reassigned with
`XAUTOCLAIM`. This is still at-least-once delivery; the stream prevents loss
of unacknowledged messages but does not provide exactly-once business effects.

Bounded concurrency should be global plus optionally per store/activity:

```text
global max workers: 4-16
per store/activity max workers: 1-4
tool timeout: 1-3s per tool
agent max steps: 4-6
```

This keeps response latency stable and prevents one hot activity from exhausting all workers.

For one review, the dependency graph is:

```text
get_review_evidence
  ├─ get_review_stats
  └─ retrieve_rag
final decision + validator + store_decision
```

Evidence must run first because it provides `store_id`, `user_id`, `sku_id`, `spu_id`, and review content. After that, ES stats and RAG retrieval are independent and can run concurrently. If either branch fails, the agent writes a degraded observation and continues:

- ES unavailable: use `risk_level=unknown` and tag `stats_unavailable`.
- RAG unavailable: use an empty chunk list and tag the reasoning as `rag_unavailable`.

LLM calls are protected by a local token bucket and a circuit breaker so high concurrency cannot directly punch through to the model provider.

## Distributed Lock and Rate Limit

For multi-instance deployment:

- Use Redis session storage for short-term context.
- Use Redis distributed lock for final decision persistence.
- Lock key: `seckill-agent:lock:decision:store:{store_id}:review:{review_id}`.
- Lock TTL: 30 seconds by default.

The lock value is a random token (not business data), acquired with `SET key token NX EX 120` (or the configured TTL). Only the owner token may release it. SKU/SPU, activity, scene, policy, and evidence versions are decision fields, not lock scope.

The lock only protects the money-moving finalization path. It does not lock evidence/stat/RAG reads, so normal read parallelism remains high.

The LLM guard is local per process:

```text
token bucket: rate_per_second + burst
circuit breaker: gobreaker
fallback: fail fast with clear unavailable/rate-limit error
```

This is intentionally simple. If deployment later requires global LLM quota, the token bucket can move to Redis, but that is not needed for the current project scale.

## Tool Orchestration Stability

The stable path is:

1. `get_review_evidence`
2. `get_review_stats`
3. `retrieve_rag`
4. `store_decision`

Rules:

- Evidence failure: stop and return retryable error.
- Stats failure: continue only if evidence is strong, mark stats unavailable.
- RAG failure: continue with evidence and rules, mark `rag_unavailable`.
- Store decision failure: retry with deterministic decision id.
- Final answer should only be emitted after `store_decision`.

## Coupon Grant Idempotency

Only call `seckill-service.GrantCouponByReview` when:

- `action == GRANT_COUPON`
- `need_human_review == false`
- validator passes
- no Redis/PostgreSQL grant record already marks success

Stable grant idempotency key:

```text
review:store:{store_id}:review:{review_id}
```

The key is independent of activity, scene, policy, evidence, SKU, and SPU, so one review in one store can receive at most one coupon. A later evidence version may update the decision and audit record, but `grant_status=GRANTED` permanently prevents another grant. The seckill service must enforce this key atomically.

Coupon templates are configured in `configs/coupon_policy.json`. The agent still decides business intent such as `PHOTO_REVIEW_REWARD` or `AFTER_SALE_COMPENSATION`; the backend policy maps that intent to seckill-service scenes:

```text
FIRST_REVIEW -> FIRST_REVIEW
GOOD_REVIEW_REWARD -> GOOD_REVIEW
PHOTO_REVIEW_REWARD -> PHOTO_REVIEW
AFTER_SALE_COMPENSATION/SERVICE_RECOVERY -> AFTER_SALE_COMPENSATION
INVITE_NEW -> INVITE_NEW
REPURCHASE_INCENTIVE/PRICE_SENSITIVE_INCENTIVE -> GOOD_REVIEW
```

This keeps coupon amounts and validity periods adjustable without changing prompts or Go decision logic.

Duplicate response handling:

- `duplicated=true, success=true`: treat as success and persist as idempotent duplicate.
- `duplicated=true, success=false`: inspect existing grant record before retrying.
- timeout after request: query/persist by idempotency key before retry.

## Validator Boundary

The LLM decides intent:

- `scene`
- `action`
- `reason`
- `risk_level`

The backend validator enforces hard constraints:

- `MANUAL_REVIEW => coupon_granted=false`
- `NO_COUPON => coupon_granted=false`
- `hasAppeal=true => action!=GRANT_COUPON`
- `score<=2 && quality_issue => action!=GRANT_COUPON`
- `GRANT_COUPON => coupon_suggestion` comes from backend coupon strategy

This preserves agent flexibility while keeping money-moving decisions safe.
