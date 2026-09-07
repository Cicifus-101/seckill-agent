package tool

import "testing"

import "seckill-agent/internal/client/reviewjob"

func TestNormalizeRAGAspectsAlwaysIncludesSummaryAndDecision(t *testing.T) {
	t.Parallel()

	got := normalizeRAGAspects([]string{"PRICE", "REPURCHASE", "RECOMMEND"})

	assertContains(t, got, "SUMMARY")
	assertContains(t, got, "DECISION")
	assertContains(t, got, "PRICE")
}

func TestNormalizeRAGChunkTypesAlwaysIncludesSummaryAndDecisionSources(t *testing.T) {
	t.Parallel()

	got := normalizeRAGChunkTypes([]string{"review_aspect"})

	assertContains(t, got, "review_summary")
	assertContains(t, got, "coupon_decision")
	assertContains(t, got, "review_aspect")
}

func TestCompactRAGChunksStripsMetadata(t *testing.T) {
	t.Parallel()

	got := compactRAGChunks([]*reviewjob.RAGChunkDTO{
		{
			ChunkID:   "review:990200002:summary",
			ReviewID:  990200002,
			StoreID:   20001,
			ChunkType: "review_summary",
			Aspect:    "SUMMARY",
			Text:      "用户给出4星评价，评价内容体现价格敏感。",
			Metadata: map[string]any{
				"large": "metadata should not be exposed to the agent prompt",
			},
		},
	})

	if len(got) != 1 {
		t.Fatalf("expected one compact chunk, got %d", len(got))
	}
	if got[0].ChunkID != "review:990200002:summary" || got[0].Text == "" {
		t.Fatalf("unexpected compact chunk: %#v", got[0])
	}
}

func assertContains(t *testing.T, items []string, want string) {
	t.Helper()

	for _, item := range items {
		if item == want {
			return
		}
	}
	t.Fatalf("expected %q in %#v", want, items)
}
