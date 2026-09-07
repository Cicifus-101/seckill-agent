# Review Coupon Agent Evaluation

## Purpose

The service is evaluated as a review-driven coupon decision system with two
boundaries:

1. `review-job` produces review facts, neutral signals, ES statistics, and
   hybrid RAG evidence.
2. `seckill-agent` gathers those tools in parallel, lets the model propose an
   intent, then applies deterministic guards, campaign constraints, idempotent
   grant execution, and decision persistence.

The evaluation must therefore answer four separate questions:

| Layer | Question | Primary metrics |
|---|---|---|
| Signal and intent | Were aspects and risk facts understood? | aspect precision/recall, signal agreement |
| Retrieval | Did hybrid RAG return useful, correctly scoped evidence? | Hit@K, Recall@K, MRR, NDCG@K, source coverage |
| Safety and isolation | Did constraints prevent an unsafe grant? | cross-store leak count, guard recall, unsafe-grant count |
| Execution | Was the final decision persisted and issued exactly once? | action consistency, execution-status consistency, duplicate-grant count |

`testdata/evaluation` is the machine-readable source of truth. Positive and
negative cases are intentionally separated because coupon systems are asymmetric:
one missed marketing reward is recoverable; one unsafe coupon grant is not.

## Corpus Design

The corpus reuses the 30 CDC fixtures in `review-job` and adds small synthetic
facts only for conditions absent from those rows, such as `is_first_review`.

Positive cases cover every configured coupon policy:

- first review, good review, photo review;
- price-sensitive, repurchase, and invite intent;
- logistics and service recovery;
- a multi-aspect review that contains both logistics and service evidence.

Negative cases cover:

- low-score quality issues and quality/price conflicts;
- appeal/dispute override and neutral no-coupon outcomes;
- store isolation;
- inactive campaign, ineligible product, zero stock, zero budget, and user quota;
- RAG/ES degradation, duplicate grant success, and retryable grant failure.

For every case, labels are split by responsibility:

- retrieval labels: expected aspects, relevant historical chunk IDs, forbidden IDs;
- decision labels: final action, allowed scene, human-review requirement;
- execution labels: status, granted flag, number of grant calls;
- safety labels: invariants such as no cross-store evidence or no second coupon.

This prevents a good final action from hiding an incorrect retrieval, and prevents
a good RAG result from hiding a broken money-moving path.

## Correctness Protocol

1. Load both MySQL seed scripts from `review-job/deployments/sql/mysql`.
2. Wait until Canal/Kafka has created ES documents and pgvector chunks.
3. Run each JSONL row using a deterministic campaign and grant-service test double.
4. Save one result record per run with input IDs, retrieved chunks, tool timings,
   final decision, execution status, and grant idempotency key.
5. Run each model-dependent case at least three times with temperature zero. For
   non-deterministic model experiments, report pass rate and confidence interval,
   not a single best result.

Retrieval acceptance is evaluated before the LLM decision:

```text
Hit@K       = queries with at least one relevant chunk in top K / all queries
Recall@K    = relevant chunks retrieved in top K / labeled relevant chunks
MRR         = mean reciprocal rank of the first relevant chunk
NDCG@K      = relevance-weighted ranking quality
```

For this service, report two retrieval results instead of collapsing the three
chunk types into a decision-only metric:

- `evidence_hit_at_k`: the top K contains any labelled `review_summary`,
  `review_aspect`, or `coupon_decision` evidence. This measures whether the
  Agent receives useful facts or precedent for the next decision step.
- `decision_hit_at_k`: the top K contains a labelled historical
  `coupon_decision`. This measures the narrower quality of precedent recall.

`decision_support` excludes the current `review_id` by design. The evaluator
rejects labels that point to the current review's summary or decision chunk;
those cases must either name a historical equivalent or be evaluated in the
decision/execution layer instead. Store-isolation cases are safety assertions,
not recall samples, and are excluded from the Recall denominator.

### Execution Boundary

This repository intentionally keeps no evaluation runner, local-vector shortcut,
or cleanup script. Start the real services, load isolated fixture data, and replay
the JSONL corpus through the actual Agent and `/v1/agent/rag/retrieve` endpoints.
The result collector belongs to the test environment rather than the application
repository, so it cannot silently replace real embeddings, PostgreSQL keyword
retrieval, ES BM25, campaign validation, or coupon execution.

Use a dedicated database or namespace for every run. Historical decision chunks
must be written through the current decision persistence path with the same
`activity_id` and `policy_version` as the case. Those fields are strict filters
for `coupon_decision` retrieval; missing metadata is a fixture/indexing defect,
not a reason to relax production filtering.

Safety acceptance is strict:

```text
cross_store_leak_count = 0
unsafe_grant_count     = 0
duplicate_grant_count  = 0
```

For negative cases, use `manual_review_precision`, `manual_review_recall`,
`no_coupon_precision`, and `grant_false_positive_rate`. For positive cases, use
`grant_recall` and `coupon_granted_consistency`.

## Performance Protocol

Use `performance_scenarios.json` with two corpus scales:

- correctness scale: 30 review fixtures, used for debugging labels;
- performance scale: 100 stores x 1,000 reviews, one decision chunk per review,
  1024-dimensional vectors, and 20 percent hot-store traffic.

Measure cold and warm RAG separately. Record vector, keyword, ES BM25, merge/
rerank, embedding, and pool-wait time; an end-to-end p95 alone cannot identify
whether the bottleneck is embedding, PostgreSQL, ES, or the model.

For Agent E2E load tests, use a fixed LLM test double first. It isolates tool
parallelism, campaign validation, locking, persistence, and grant retries from
external model latency. Run a separate production-model experiment afterwards and
report model-provider latency as its own component.

The JSON budgets are release gates, not claimed benchmark numbers. A result report
must include commit SHA, policy version, corpus version, hardware, Docker resource
limits, cache state, concurrency, p50/p95/p99, error/degraded rate, and all safety
counters.

## Expected Improvements To Validate

The current architecture already has three useful performance levers:

- evidence is fetched first; stats, RAG, and campaign context then run in parallel;
- RAG uses vector, PostgreSQL keyword, and ES BM25 candidates with RRF merging;
- grant execution uses a deterministic key and finalization lock.

Use the scenarios to validate improvements rather than asserting them in advance:

- add `(store_id, sku_id)` and `(store_id, spu_id)` indexes when scope-specific
  retrieval shows high scanned rows or p95 degradation;
- tune `candidate_limit` and `top_k` when merge/rerank dominates latency;
- tune connection pools and HNSW parameters only after separating PG pool wait
  from vector-search time;
- bound per-store/activity concurrency when hot-store contention increases lock
  conflicts or grant retries;
- keep fallback rates visible: graceful degradation is not success if it becomes
  the normal path.

## Interview Summary

The project does not claim that an LLM alone decides coupons. The evaluation proves
that review facts and neutral signals are understood, hybrid RAG retrieves relevant
and isolated evidence, deterministic guards and campaign constraints block unsafe
proposals, and idempotent execution prevents repeated coupon issuance. Performance
is reported component-by-component with reproducible traffic profiles and explicit
safety counters, rather than with a single opaque QPS number.
