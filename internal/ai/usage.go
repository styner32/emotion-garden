package ai

import (
	"emotion-garden/internal/config"

	"github.com/shopspring/decimal"
)

var oneMillion = decimal.NewFromInt(1_000_000)

// EstimateCost calculates the decimal USD cost for a run when usage and price rules
// are known. It strictly avoids double-counting OpenAI reasoning tokens when
// reasoning_included_in_output is true, and returns nil (unknown) rather than 0
// when usage or rate rules are missing (Spec Section 15, AI11).
func EstimateCost(reg *config.ModelRegistry, entry config.ModelEntry, u *Usage) (*decimal.Decimal, string) {
	if u == nil || u.Status == "" || u.Status == "unknown" || u.InputTokens == nil {
		return nil, "unknown"
	}
	outTok := u.OutputTokens
	if outTok == nil {
		outTok = u.PublicOutputTokens
	}
	if outTok == nil {
		return nil, u.Status
	}

	rule, ok := reg.LookupRate(entry.PriceVersion, entry.Provider, entry.ModelID, entry.APIVariant)
	if !ok {
		return nil, u.Status
	}

	inputTotal := *u.InputTokens
	var cachedIn int64
	if u.CachedInputTokens != nil && *u.CachedInputTokens > 0 {
		cachedIn = *u.CachedInputTokens
	}
	var cacheWrite int64
	if u.CacheWriteTokens != nil && *u.CacheWriteTokens > 0 {
		cacheWrite = *u.CacheWriteTokens
	}

	regularIn := inputTotal - cachedIn - cacheWrite
	if regularIn < 0 {
		regularIn = 0
	}

	cost := decimal.NewFromInt(regularIn).Mul(rule.InputPer1M).Div(oneMillion)
	if cachedIn > 0 {
		cost = cost.Add(decimal.NewFromInt(cachedIn).Mul(rule.CachedInputPer1M).Div(oneMillion))
	}
	if cacheWrite > 0 {
		cost = cost.Add(decimal.NewFromInt(cacheWrite).Mul(rule.CacheWritePer1M).Div(oneMillion))
	}

	cost = cost.Add(decimal.NewFromInt(*outTok).Mul(rule.OutputPer1M).Div(oneMillion))

	if !rule.ReasoningIncludedInOutput && u.ReasoningTokens != nil && *u.ReasoningTokens > 0 {
		cost = cost.Add(decimal.NewFromInt(*u.ReasoningTokens).Mul(rule.ReasoningPer1M).Div(oneMillion))
	}

	rounded := cost.Round(8)
	return &rounded, u.Status
}
