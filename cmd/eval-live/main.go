package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/ai/gemini"
	"emotion-garden/internal/ai/mock"
	"emotion-garden/internal/ai/openai"
	"emotion-garden/internal/config"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type evalFile struct {
	Version   string         `json:"version"`
	Scenarios []evalScenario `json:"scenarios"`
}

type evalScenario struct {
	ID          string                     `json:"id"`
	Kind        string                     `json:"kind"`
	Title       string                     `json:"title"`
	History     []ai.TranscriptHistoryItem `json:"history"`
	UserMessage string                     `json:"user_message"`
}

func main() {
	providerFlag := flag.String("provider", "mock_openai", "Provider to evaluate: mock_openai, mock_gemini, openai, gemini")
	fixturePath := flag.String("fixtures", "./tests/fixtures/eval_scenarios.json", "Path to evaluation scenarios JSON")
	maxCases := flag.Int("max-cases", 24, "Maximum number of scenarios to run")
	maxCostUSD := flag.Float64("max-cost-usd", 0.50, "Hard ceiling on cumulative estimated USD cost")
	flag.Parse()

	bundle, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load error: %v\n", err)
		os.Exit(1)
	}

	raw, err := os.ReadFile(*fixturePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read fixtures error: %v\n", err)
		os.Exit(1)
	}
	var ef evalFile
	if err := json.Unmarshal(raw, &ef); err != nil {
		fmt.Fprintf(os.Stderr, "parse fixtures error: %v\n", err)
		os.Exit(1)
	}

	var adapter ai.ChatProvider
	alias := "openai_chat"
	switch *providerFlag {
	case "openai":
		adapter = openai.NewAdapter(bundle.Env.OpenAIAPIKey, bundle.Env.OpenAIBaseURL, nil)
		alias = "openai_chat"
	case "gemini":
		adapter = gemini.NewAdapter(bundle.Env.GeminiAPIKey, bundle.Env.GeminiBaseURL, nil)
		alias = "gemini_chat"
	case "mock_gemini":
		adapter = mock.NewAdapter("mock_gemini", 5*time.Millisecond, nil)
		alias = "gemini_chat"
	default:
		adapter = mock.NewAdapter("mock_openai", 5*time.Millisecond, nil)
		alias = "openai_chat"
	}

	entry, _ := bundle.Models.GetByAlias(alias)
	budgetCap := decimal.NewFromFloat(*maxCostUSD)
	spent := decimal.Zero

	count := 0
	for _, sc := range ef.Scenarios {
		if count >= *maxCases {
			break
		}
		if spent.GreaterThanOrEqual(budgetCap) {
			fmt.Printf("Stopping: cost cap $%.4f reached (spent=$%s)\n", *maxCostUSD, spent.StringFixed(6))
			break
		}
		count++

		built, err := ai.BuildPortableContext(sc.Kind, "상대", sc.History, sc.UserMessage, 1, 100, 131072)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] context error: %v\n", sc.ID, err)
			continue
		}
		sysPrompt := bundle.Prompts.GrowPrompt
		if sc.Kind == "release" {
			sysPrompt = bundle.Prompts.ReleasePrompt
		}

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := adapter.Generate(ctx, ai.GenerationRequest{
			RunID:            uuid.New(),
			ConversationKind: sc.Kind,
			Provider:         *providerFlag,
			ModelAlias:       alias,
			ModelID:          entry.ModelID,
			SystemPrompt:     sysPrompt,
			ContextJSON:      built.JSONBytes,
			ContextHash:      built.Hash,
			Options: ai.GenerationOptions{
				MaxOutputTokens: entry.MaxOutputTokens,
				ReasoningLevel:  entry.DefaultReasoningLevel,
			},
		}, func(ai.ProviderEvent) error { return nil })
		cancel()

		cost, _ := ai.EstimateCost(bundle.Models, entry, res.Usage)
		if cost != nil {
			spent = spent.Add(*cost)
		}
		fmt.Printf("[%s | %s | %s] status=%s err=%v\n  Q: %s\n  A: %s\n\n",
			sc.ID, sc.Kind, sc.Title, res.Status, err, sc.UserMessage, res.OutputText)
	}
	fmt.Printf("Completed %d scenarios; total estimated cost = $%s\n", count, spent.StringFixed(6))
}
