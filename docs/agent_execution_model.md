# Agent Execution Model

This project treats the agent as a merchant-operation decision service, not as a consumer-facing chat session.

## IDs

- `session_id`: business batch/window. It groups related review-coupon decisions for one store, scene/campaign, and time window.
- `job_id`: async task id. It is used by callers to submit and query one unit of work.
- `execution_id`: one agent execution context. Tool history and recent steps are stored per `session_id + execution_id`, not only per `session_id`.
- `parent_execution_id`: previous failed execution for retry traceability.

Recommended session shapes:

- Campaign: `store:{store_id}:campaign:{campaign_id}:window:{yyyyMMdd}`
- Daily/manual scene: `store:{store_id}:scene:{scene}:window:{yyyyMMdd}`

## Trigger Types

The job API accepts `trigger_type` as lightweight business metadata:

- `campaign_warmup`: batch decisions before a seckill/campaign starts.
- `realtime_review`: selected review events during an activity.
- `daily_batch`: scheduled daily review-care task.
- `manual`: merchant/backend operator manually submits a review decision.

The agent should not be triggered for every new review. Upstream business rules first select candidate reviews based on store policy, campaign, rating, appeal status, coupon budget, recent grant history, and review state.

## Memory Boundary

Memory is execution-scoped:

```text
session_id + execution_id -> Summary + RecentSteps
```

This allows many jobs under one business session without mixing tool results. `session_id` groups the batch; `execution_id` owns the workflow trace.

## Retry

Retries use clean execution windows:

```text
execution_1: FAILED
execution_2: RETRYING, parent_execution_id=execution_1
```

The retry keeps the same `job_id` and `session_id`, generates a new `execution_id`, and does not reuse the failed execution's full `RecentSteps`.
There is no public reset flag in the job API because a new `execution_id` already gives each attempt a clean context.

## Grant And Store Consistency

Coupon granting and decision persistence are not one database transaction. The project keeps the solution simple:

- Coupon grant uses a stable idempotency key derived from review and policy scene.
- If final decision persistence fails, the agent returns a finalizer error so the job layer retries with a new execution.
- If the coupon was already granted in a previous attempt, the grant service should return duplicated/success through the same idempotency key instead of issuing another coupon.
- Decision output stores the idempotency key and grant outcome for audit and compensation.
