package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

type EnvConfig struct {
	AppEnv                  string
	HTTPAddr                string
	PublicOrigin            string
	ExtraAllowedOrigins     []string
	AuthMode                string
	DatabaseURL             string
	IdempotencySecret       string
	OpenAIAPIKey            string
	GeminiAPIKey            string
	AllowMockAI             bool
	OpenAIBaseURL           string
	GeminiBaseURL           string
	ModelConfigPath         string
	AvatarCatalogPath       string
	FlowerCatalogPath       string
	RitualConfigPath        string
	PromptsDir              string
	StaticDir               string
	AIWorkerConcurrency     int
	AIRunTimeout            time.Duration
	AIFirstTextTimeout      time.Duration
	AIIdleTimeout           time.Duration
	GardenDefaultCols       int16
	GardenDefaultRows       int16
	GardenMaxCols           int16
	GardenMaxRows           int16
	DefaultTimezone         string
	ContentContextMaxBytes  int
	ConversationMaxTurns    int
	IdempotencyRetention    time.Duration
	UsageRetention          time.Duration
}

type ModelEntry struct {
	Alias                    string   `yaml:"alias" json:"alias"`
	DisplayName              string   `yaml:"display_name" json:"display_name"`
	Provider                 string   `yaml:"provider" json:"provider"`
	ModelID                  string   `yaml:"model_id" json:"model_id"`
	APIVariant               string   `yaml:"api_variant" json:"api_variant"`
	APIRevision              string   `yaml:"api_revision" json:"api_revision"`
	Enabled                  bool     `yaml:"enabled" json:"enabled"`
	DefaultReasoningLevel    string   `yaml:"default_reasoning_level" json:"default_reasoning_level"`
	SupportedReasoningLevels []string `yaml:"supported_reasoning_levels" json:"supported_reasoning_levels"`
	SupportsTemperature      bool     `yaml:"supports_temperature" json:"supports_temperature"`
	SupportsTopP             bool     `yaml:"supports_top_p" json:"supports_top_p"`
	MaxInputTokens           int64    `yaml:"max_input_tokens" json:"max_input_tokens"`
	MaxOutputTokens          int64    `yaml:"max_output_tokens" json:"max_output_tokens"`
	PriceVersion             string   `yaml:"price_version" json:"price_version"`
}

type RateRule struct {
	InputPer1M                string `yaml:"input_per_1m"`
	CachedInputPer1M          string `yaml:"cached_input_per_1m"`
	CacheWritePer1M           string `yaml:"cache_write_per_1m"`
	OutputPer1M               string `yaml:"output_per_1m"`
	ReasoningPer1M            string `yaml:"reasoning_per_1m"`
	ReasoningIncludedInOutput bool   `yaml:"reasoning_included_in_output"`
}

type ParsedRateRule struct {
	InputPer1M                decimal.Decimal
	CachedInputPer1M          decimal.Decimal
	CacheWritePer1M           decimal.Decimal
	OutputPer1M               decimal.Decimal
	ReasoningPer1M            decimal.Decimal
	ReasoningIncludedInOutput bool
}

type PriceRuleSet struct {
	PriceVersion  string              `yaml:"price_version"`
	Currency      string              `yaml:"currency"`
	EffectiveFrom string              `yaml:"effective_from"`
	Rates         map[string]RateRule `yaml:"rates"`
}

type ModelRegistry struct {
	RegistryVersion    string                    `yaml:"registry_version" json:"registry_version"`
	DefaultReleaseMode string                    `yaml:"default_release_mode" json:"default_release_mode"`
	DefaultGrowMode    string                    `yaml:"default_grow_mode" json:"default_grow_mode"`
	Models             []ModelEntry              `yaml:"models" json:"models"`
	PriceRules         []PriceRuleSet            `yaml:"price_rules" json:"-"`
	byAlias            map[string]ModelEntry     `yaml:"-" json:"-"`
	parsedRates        map[string]ParsedRateRule `yaml:"-" json:"-"`
}

func (r *ModelRegistry) GetByAlias(alias string) (ModelEntry, bool) {
	m, ok := r.byAlias[alias]
	return m, ok && m.Enabled
}

func (r *ModelRegistry) LookupRate(priceVersion, provider, modelID, apiVariant string) (ParsedRateRule, bool) {
	key := fmt.Sprintf("%s|%s:%s:%s", priceVersion, provider, modelID, apiVariant)
	rule, ok := r.parsedRates[key]
	return rule, ok
}

type FacePreset struct {
	ID             string   `yaml:"id" json:"id"`
	Label          string   `yaml:"label" json:"label"`
	Description    string   `yaml:"description" json:"description"`
	RecommendedFor []string `yaml:"recommended_for" json:"recommended_for"`
}

type SimpleOption struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
}

type SkinTone struct {
	ID           string `yaml:"id" json:"id"`
	Label        string `yaml:"label" json:"label"`
	BaseHex      string `yaml:"base_hex" json:"base_hex"`
	ShadowHex    string `yaml:"shadow_hex" json:"shadow_hex"`
	HighlightHex string `yaml:"highlight_hex" json:"highlight_hex"`
}

type GreetingOption struct {
	Key  string `yaml:"key" json:"key"`
	Text string `yaml:"text" json:"text"`
}

type GreetingGroup struct {
	DefaultKey string           `yaml:"default_key" json:"default_key"`
	Options    []GreetingOption `yaml:"options" json:"options"`
}

type AvatarCatalog struct {
	CatalogVersion  string                   `yaml:"catalog_version" json:"catalog_version"`
	SchemaVersion   string                   `yaml:"schema_version" json:"schema_version"`
	FacePresets     []FacePreset             `yaml:"face_presets" json:"face_presets"`
	HairStyles      []SimpleOption           `yaml:"hair_styles" json:"hair_styles"`
	HairPalette     []string                 `yaml:"hair_palette" json:"hair_palette"`
	SkinTones       []SkinTone               `yaml:"skin_tones" json:"skin_tones"`
	EyeStyles       []SimpleOption           `yaml:"eye_styles" json:"eye_styles"`
	FeatureLevels   []SimpleOption           `yaml:"feature_levels" json:"feature_levels"`
	ClothingPalette []string                 `yaml:"clothing_palette" json:"clothing_palette"`
	Expressions     []SimpleOption           `yaml:"expressions" json:"expressions"`
	Greetings       map[string]GreetingGroup `yaml:"greetings" json:"greetings"`
}

func (c *AvatarCatalog) ResolveGreeting(kind, key string) (string, string, bool) {
	grp, ok := c.Greetings[kind]
	if !ok {
		return "", "", false
	}
	if key == "" {
		key = grp.DefaultKey
	}
	for _, opt := range grp.Options {
		if opt.Key == key {
			return opt.Key, opt.Text, true
		}
	}
	return "", "", false
}

type FlowerSpecies struct {
	ID        string `yaml:"id" json:"id"`
	NameKo    string `yaml:"name_ko" json:"name_ko"`
	Weight    int    `yaml:"weight" json:"weight"`
	PetalHex  string `yaml:"petal_hex" json:"petal_hex"`
	CenterHex string `yaml:"center_hex" json:"center_hex"`
	StemHex   string `yaml:"stem_hex" json:"stem_hex"`
}

type FlowerCatalog struct {
	CatalogVersion string          `yaml:"catalog_version" json:"catalog_version"`
	Species        []FlowerSpecies `yaml:"species" json:"species"`
}

type RitualActionDef struct {
	Key                  string `yaml:"key" json:"key"`
	Label                string `yaml:"label" json:"label"`
	DurationMs           int64  `yaml:"duration_ms" json:"duration_ms"`
	VisualStage          int    `yaml:"visual_stage" json:"visual_stage"`
	CompletedVisualStage int    `yaml:"completed_visual_stage" json:"completed_visual_stage"`
	Description          string `yaml:"description" json:"description"`
}

type RitualRouteDef struct {
	Kind        string            `yaml:"kind" json:"kind"`
	Label       string            `yaml:"label" json:"label"`
	CloseReason string            `yaml:"close_reason,omitempty" json:"close_reason,omitempty"`
	Actions     []RitualActionDef `yaml:"actions" json:"actions"`
}

type RitualCatalog struct {
	DefinitionVersion string                    `yaml:"definition_version" json:"definition_version"`
	Routes            map[string]RitualRouteDef `yaml:"routes" json:"routes"`
}

type PromptBundle struct {
	Version       string
	CommonText    string
	ReleaseText   string
	GrowText      string
	ReleasePrompt string
	ReleaseHash   string
	GrowPrompt    string
	GrowHash      string
}

type Bundle struct {
	Env      EnvConfig
	Models   *ModelRegistry
	Avatars  *AvatarCatalog
	Flowers  *FlowerCatalog
	Rituals  *RitualCatalog
	Prompts  *PromptBundle
}

func LoadFromEnv() (*Bundle, error) {
	env := EnvConfig{
		AppEnv:                 getEnv("APP_ENV", "development"),
		HTTPAddr:               getEnv("HTTP_ADDR", "127.0.0.1:8080"),
		PublicOrigin:           getEnv("PUBLIC_ORIGIN", "http://localhost:8080"),
		AuthMode:               getEnv("AUTH_MODE", "single_user"),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://app:change_me@127.0.0.1:5432/emotion_garden?sslmode=disable"),
		IdempotencySecret:      getEnv("IDEMPOTENCY_SECRET", "dev-emotion-garden-hmac-secret"),
		OpenAIAPIKey:           strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		GeminiAPIKey:           strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		AllowMockAI:            getEnvBool("ALLOW_MOCK_AI", true),
		OpenAIBaseURL:          strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")),
		GeminiBaseURL:          getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		ModelConfigPath:        getEnv("AI_MODEL_CONFIG", "./config/models.yaml"),
		AvatarCatalogPath:      getEnv("AVATAR_CATALOG_CONFIG", "./config/avatars.yaml"),
		FlowerCatalogPath:      getEnv("FLOWER_CATALOG_CONFIG", "./config/flowers.yaml"),
		RitualConfigPath:       getEnv("RITUAL_CONFIG", "./config/rituals.yaml"),
		PromptsDir:             getEnv("PROMPTS_DIR", "./prompts"),
		StaticDir:              getEnv("STATIC_DIR", "./web/dist"),
		AIWorkerConcurrency:    getEnvInt("AI_WORKER_CONCURRENCY", 4),
		AIRunTimeout:           time.Duration(getEnvInt("AI_RUN_TIMEOUT_SECONDS", 60)) * time.Second,
		AIFirstTextTimeout:     time.Duration(getEnvInt("AI_FIRST_TEXT_TIMEOUT_SECONDS", 25)) * time.Second,
		AIIdleTimeout:          time.Duration(getEnvInt("AI_IDLE_TIMEOUT_SECONDS", 20)) * time.Second,
		GardenDefaultCols:      int16(getEnvInt("GARDEN_DEFAULT_COLS", 4)),
		GardenDefaultRows:      int16(getEnvInt("GARDEN_DEFAULT_ROWS", 3)),
		GardenMaxCols:          int16(getEnvInt("GARDEN_MAX_COLS", 6)),
		GardenMaxRows:          int16(getEnvInt("GARDEN_MAX_ROWS", 8)),
		DefaultTimezone:        getEnv("DEFAULT_TIMEZONE", "Asia/Seoul"),
		ContentContextMaxBytes: getEnvInt("CONTENT_CONTEXT_MAX_BYTES", 131072),
		ConversationMaxTurns:   getEnvInt("CONVERSATION_MAX_TURNS", 100),
		IdempotencyRetention:   time.Duration(getEnvInt("IDEMPOTENCY_RETENTION_DAYS", 7)) * 24 * time.Hour,
		UsageRetention:         time.Duration(getEnvInt("USAGE_RETENTION_DAYS", 90)) * 24 * time.Hour,
	}

	extraOrigins := getEnv("EXTRA_ALLOWED_ORIGINS", "http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173")
	for _, item := range strings.Split(extraOrigins, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			env.ExtraAllowedOrigins = append(env.ExtraAllowedOrigins, trimmed)
		}
	}

	models, err := loadModels(env.ModelConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load models config: %w", err)
	}
	avatars, err := loadYAML[AvatarCatalog](env.AvatarCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("load avatars catalog: %w", err)
	}
	flowers, err := loadYAML[FlowerCatalog](env.FlowerCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("load flowers catalog: %w", err)
	}
	rituals, err := loadYAML[RitualCatalog](env.RitualConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load rituals config: %w", err)
	}
	prompts, err := loadPrompts(env.PromptsDir)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}

	return &Bundle{
		Env:     env,
		Models:  models,
		Avatars: avatars,
		Flowers: flowers,
		Rituals: rituals,
		Prompts: prompts,
	}, nil
}

func loadYAML[T any](path string) (*T, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out T
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func loadModels(path string) (*ModelRegistry, error) {
	reg, err := loadYAML[ModelRegistry](path)
	if err != nil {
		return nil, err
	}
	reg.byAlias = make(map[string]ModelEntry, len(reg.Models))
	for _, m := range reg.Models {
		reg.byAlias[m.Alias] = m
	}
	reg.parsedRates = make(map[string]ParsedRateRule)
	for _, ps := range reg.PriceRules {
		for rateKey, rule := range ps.Rates {
			inRate, err := decimal.NewFromString(rule.InputPer1M)
			if err != nil {
				return nil, fmt.Errorf("invalid input_per_1m for %s: %w", rateKey, err)
			}
			cachedRate, err := decimal.NewFromString(rule.CachedInputPer1M)
			if err != nil {
				return nil, fmt.Errorf("invalid cached_input_per_1m for %s: %w", rateKey, err)
			}
			writeRate, err := decimal.NewFromString(rule.CacheWritePer1M)
			if err != nil {
				return nil, fmt.Errorf("invalid cache_write_per_1m for %s: %w", rateKey, err)
			}
			outRate, err := decimal.NewFromString(rule.OutputPer1M)
			if err != nil {
				return nil, fmt.Errorf("invalid output_per_1m for %s: %w", rateKey, err)
			}
			reasonRate, err := decimal.NewFromString(rule.ReasoningPer1M)
			if err != nil {
				return nil, fmt.Errorf("invalid reasoning_per_1m for %s: %w", rateKey, err)
			}
			reg.parsedRates[fmt.Sprintf("%s|%s", ps.PriceVersion, rateKey)] = ParsedRateRule{
				InputPer1M:                inRate,
				CachedInputPer1M:          cachedRate,
				CacheWritePer1M:           writeRate,
				OutputPer1M:               outRate,
				ReasoningPer1M:            reasonRate,
				ReasoningIncludedInOutput: rule.ReasoningIncludedInOutput,
			}
		}
	}
	return reg, nil
}

func loadPrompts(dir string) (*PromptBundle, error) {
	commonBytes, err := os.ReadFile(filepath.Join(dir, "system_common_v1.txt"))
	if err != nil {
		return nil, err
	}
	releaseBytes, err := os.ReadFile(filepath.Join(dir, "role_release_v1.txt"))
	if err != nil {
		return nil, err
	}
	growBytes, err := os.ReadFile(filepath.Join(dir, "role_grow_v1.txt"))
	if err != nil {
		return nil, err
	}
	common := strings.TrimSpace(string(commonBytes))
	rel := strings.TrimSpace(string(releaseBytes))
	grw := strings.TrimSpace(string(growBytes))

	relCombined := common + "\n\n" + rel
	grwCombined := common + "\n\n" + grw

	return &PromptBundle{
		Version:       "prompt_v1",
		CommonText:    common,
		ReleaseText:   rel,
		GrowText:      grw,
		ReleasePrompt: relCombined,
		ReleaseHash:   sha256Hex([]byte(relCombined)),
		GrowPrompt:    grwCombined,
		GrowHash:      sha256Hex([]byte(grwCombined)),
	}, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
