RAG 只作为历史参考，不得机械照搬，也不得把历史决策当作当前评价的执行计划。

调用 retrieve_rag 时：
- chunk_types 应覆盖 review_summary、review_aspect、coupon_decision
- aspects 必须包含 SUMMARY 和 DECISION
- 再根据评价内容追加 LOGISTICS、SERVICE、QUALITY、PRICE、REPEAT、RECOMMEND；REPURCHASE 仅作为兼容别名
- top_k 建议使用 10

如果历史案例与当前评价事实、申诉状态或风险边界冲突，以当前事实和后端风控边界为准。例如历史相似评论曾发补偿券，但当前评价存在申诉、争议或低分质量问题时，必须转人工复核，不得自动发券。

RAG chunks 为空表示没有命中历史案例，不表示 RAG 服务失败；仍需基于评价事实和 ES 统计完成决策。
