# Review Coupon Evaluation Corpus

This is the single authoritative evaluation corpus for the current
`seckill-agent` repository. It evaluates the review-coupon Agent as a layered
system, not as a single LLM prompt. Do not modify production rules to improve
the score; a failing case is evidence that the implementation and the business
contract are not aligned.

- `review_coupon_positive.jsonl`: cases that are eligible to grant after
  evidence, RAG, campaign validation, and execution all succeed.
- `review_coupon_negative.jsonl`: cases that must not auto-grant, including
  quality disputes, campaign constraints, isolation, degraded dependencies,
  retries, and duplicate requests.
- `performance_scenarios.json`: reproducible workload definitions and latency
  budgets. Budgets are acceptance targets, never measured results.

Each JSONL row is one evaluation case. `review_fixture` references the CDC seed
rows under `review-job/deployments/sql/mysql`; `synthetic_evidence` is used only
when a policy condition is not represented by a seed row. A runner should:

1. load the review and historical-decision SQL fixtures and wait for CDC/RAG indexing;
2. stub `get_campaign_context` and the coupon grant service from `campaign` and
   `fault_injection`;
3. call the Agent job endpoint or the finalizer with the case input;
4. assert every `expected` field and write a result row with durations, retrieved
   chunk IDs, final action, execution status, and grant calls.

`must_retrieve_any` is intentionally an OR set: vector ranking can legitimately
change among equivalent historical cases. `must_not_retrieve` and safety
invariants are strict.

## Code-to-case alignment

The labels are derived from the current implementation boundaries:

- `configs/coupon_policy.json` defines the eight coupon scenes and their
  qualification conditions.
- Positive cases assert `allowed_seckill_scenes`, the downstream scene
  after the policy provider maps business scenes such as
  `REPURCHASE_INCENTIVE` and `PRICE_SENSITIVE_INCENTIVE` to the actual
  seckill scene.
- `internal/agent/agent.go` defines the evidence → stats/RAG/campaign → final
  workflow and tool-order guard.
- `internal/agent/decision_guard.go` defines appeal and low-score quality
  overrides.
- `internal/agent/review_coupon_finalizer.go` defines campaign hard constraints,
  grant retry, duplicate-response handling, decision persistence, and final
  execution status.
- `internal/agent/grant_idempotency.go` defines the review/store idempotency
  key.
- `internal/job/service.go` defines SourceEventID deduplication, Stream
  delivery, claim recovery, CAS updates, and dead-letter behavior.

Cases `NEG-015` through `NEG-019` intentionally cover infrastructure and
recovery contracts that may currently fail: duplicate source events, memory
write failure, decision-store failure, ambiguous grant timeout, and lock lease
loss. They are not expected-answer shortcuts; they are regression gates for
the missing or incomplete behavior.

The authoritative corpus currently contains 8 positive and 19 negative cases.
The performance file contains 6 reproducible scenarios. There is intentionally
no Python evaluator or generated result file in this directory: start the
project, replay these cases through the real endpoints and test doubles, then
store the raw result outside the repository or in a separately versioned
artifact.
