package contextx

import "unicode/utf8"

type Budget struct {
	MaxPromptTokens int
	MaxRecentSteps  int
	MaxSummaryChars int
}

func DefaultBudget() Budget {
	return Budget{
		MaxPromptTokens: 2000,
		MaxRecentSteps:  6,
		MaxSummaryChars: 1200,
	}
}

func ApproxTokens(value string) int {
	if value == "" {
		return 0
	}
	return utf8.RuneCountInString(value)/4 + 1
}

func trimText(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
