package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv string

	LogLevel string

	DatabaseURL            string
	DatabaseMaxConns       int32
	DatabaseMinConns       int32
	DatabaseConnectTimeout time.Duration
	DatabaseQueryTimeout   time.Duration

	VK             VKConfig
	Manual         ManualTriggerConfig
	Summary        SummaryConfig
	LLM            LLMConfig
	ImagePromptLLM LLMConfig
	Image          ImageConfig
}

type VKConfig struct {
	GroupID        int64
	AccessToken    string
	APIVersion     string
	LongPollWait   int
	RequestTimeout time.Duration
	SendRandomID   int
}

type SummaryConfig struct {
	BatchSize          int
	MaxContextChars    int
	MaxContextMessages int
	MinMessageLength   int
}

type ManualTriggerConfig struct {
	UserIDs      []int64
	UserIDsSet   map[int64]struct{}
	Command      string
	DebugCommand string
}

type LLMConfig struct {
	Provider        string
	RequestTimeout  time.Duration
	MaxRetries      int
	RetryBaseDelay  time.Duration
	Model           string
	BaseURL         string
	APIKey          string
	Temperature     float64
	MaxOutputTokens int
	PromptMaxChars  int
}

type ImageConfig struct {
	Enabled        bool
	Provider       string
	BaseURL        string
	APIKey         string
	FolderID       string
	AccountID      string
	Model          string
	Quality        string
	Timeout        time.Duration
	PollInterval   time.Duration
	WidthRatio     int
	HeightRatio    int
	Width          int
	Height         int
	PromptMaxChars int
}

type envLoader struct {
	err error
}

func (l *envLoader) setErr(err error) {
	if l.err == nil {
		l.err = err
	}
}

func Load() (Config, error) {
	loader := envLoader{}
	llmCfg := LLMConfig{
		Provider:        loader.getString("LLM_PROVIDER", "stub"),
		RequestTimeout:  loader.getDuration("LLM_REQUEST_TIMEOUT", 600*time.Second),
		MaxRetries:      loader.getInt("LLM_MAX_RETRIES", 2),
		RetryBaseDelay:  loader.getDuration("LLM_RETRY_BASE_DELAY", 2*time.Second),
		Model:           loader.getString("LLM_MODEL", "stub-sarcasm-v1"),
		BaseURL:         strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/"),
		APIKey:          os.Getenv("LLM_API_KEY"),
		Temperature:     loader.getFloat("LLM_TEMPERATURE", 0.3),
		MaxOutputTokens: loader.getInt("LLM_MAX_OUTPUT_TOKENS", 220),
		PromptMaxChars:  loader.getInt("LLM_PROMPT_MAX_CHARS", 12000),
	}
	imagePromptLLMCfg := LLMConfig{
		Provider:        loader.getString("SUMMARY_IMAGE_PROMPT_LLM_PROVIDER", llmCfg.Provider),
		RequestTimeout:  loader.getDuration("SUMMARY_IMAGE_PROMPT_LLM_REQUEST_TIMEOUT", 120*time.Second),
		MaxRetries:      loader.getInt("SUMMARY_IMAGE_PROMPT_LLM_MAX_RETRIES", llmCfg.MaxRetries),
		RetryBaseDelay:  loader.getDuration("SUMMARY_IMAGE_PROMPT_LLM_RETRY_BASE_DELAY", llmCfg.RetryBaseDelay),
		Model:           loader.getString("SUMMARY_IMAGE_PROMPT_LLM_MODEL", defaultImagePromptLLMModel(llmCfg)),
		BaseURL:         strings.TrimRight(loader.getString("SUMMARY_IMAGE_PROMPT_LLM_BASE_URL", llmCfg.BaseURL), "/"),
		APIKey:          loader.getString("SUMMARY_IMAGE_PROMPT_LLM_API_KEY", llmCfg.APIKey),
		Temperature:     loader.getFloat("SUMMARY_IMAGE_PROMPT_LLM_TEMPERATURE", 0.4),
		MaxOutputTokens: loader.getInt("SUMMARY_IMAGE_PROMPT_LLM_MAX_OUTPUT_TOKENS", 220),
		PromptMaxChars:  loader.getInt("SUMMARY_IMAGE_PROMPT_LLM_PROMPT_MAX_CHARS", llmCfg.PromptMaxChars),
	}
	cfg := Config{
		AppEnv:                 loader.getString("APP_ENV", "dev"),
		LogLevel:               loader.getString("LOG_LEVEL", "INFO"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		DatabaseMaxConns:       int32(loader.getInt("DB_MAX_CONNS", 10)),
		DatabaseMinConns:       int32(loader.getInt("DB_MIN_CONNS", 1)),
		DatabaseConnectTimeout: loader.getDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
		DatabaseQueryTimeout:   loader.getDuration("DB_QUERY_TIMEOUT", 5*time.Second),
		VK: VKConfig{
			GroupID:        loader.getInt64("VK_GROUP_ID", 0),
			AccessToken:    os.Getenv("VK_ACCESS_TOKEN"),
			APIVersion:     loader.getString("VK_API_VERSION", "5.199"),
			LongPollWait:   loader.getInt("VK_LONGPOLL_WAIT", 25),
			RequestTimeout: loader.getDuration("VK_REQUEST_TIMEOUT", 20*time.Second),
			SendRandomID:   loader.getInt("VK_SEND_RANDOM_ID", 0),
		},
		Manual: ManualTriggerConfig{
			UserIDs:      loader.getInt64List("MANUAL_TRIGGER_USER_IDS"),
			Command:      loader.getString("MANUAL_TRIGGER_COMMAND", "/summary"),
			DebugCommand: loader.getString("DEBUG_COMMAND", "/livanda-debug"),
		},
		Summary: SummaryConfig{
			BatchSize:          loader.getInt("SUMMARY_BATCH_SIZE", 200),
			MaxContextChars:    loader.getInt("SUMMARY_MAX_CONTEXT_CHARS", 12000),
			MaxContextMessages: loader.getInt("SUMMARY_MAX_CONTEXT_MESSAGES", 200),
			MinMessageLength:   loader.getInt("SUMMARY_MIN_MESSAGE_LENGTH", 3),
		},
		LLM:            llmCfg,
		ImagePromptLLM: imagePromptLLMCfg,
		Image: ImageConfig{
			Enabled:        loader.getBool("SUMMARY_IMAGE_ENABLED", false),
			Provider:       loader.getString("SUMMARY_IMAGE_PROVIDER", "yandex_art"),
			BaseURL:        strings.TrimRight(loader.getString("SUMMARY_IMAGE_BASE_URL", "https://ai.api.cloud.yandex.net"), "/"),
			APIKey:         loader.getString("SUMMARY_IMAGE_API_KEY", os.Getenv("LLM_API_KEY")),
			FolderID:       loader.getString("SUMMARY_IMAGE_FOLDER_ID", folderIDFromModelURI(os.Getenv("LLM_MODEL"))),
			AccountID:      os.Getenv("SUMMARY_IMAGE_ACCOUNT_ID"),
			Model:          loader.getString("SUMMARY_IMAGE_MODEL", "yandex-art"),
			Quality:        loader.getString("SUMMARY_IMAGE_QUALITY", "medium"),
			Timeout:        loader.getDuration("SUMMARY_IMAGE_TIMEOUT", 90*time.Second),
			PollInterval:   loader.getDuration("SUMMARY_IMAGE_POLL_INTERVAL", 3*time.Second),
			WidthRatio:     loader.getInt("SUMMARY_IMAGE_WIDTH_RATIO", 1),
			HeightRatio:    loader.getInt("SUMMARY_IMAGE_HEIGHT_RATIO", 1),
			Width:          loader.getInt("SUMMARY_IMAGE_WIDTH", 1024),
			Height:         loader.getInt("SUMMARY_IMAGE_HEIGHT", 1024),
			PromptMaxChars: loader.getInt("SUMMARY_IMAGE_PROMPT_MAX_CHARS", 1200),
		},
	}

	if loader.err != nil {
		return Config{}, loader.err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.VK.GroupID <= 0 {
		return fmt.Errorf("VK_GROUP_ID is required")
	}
	if c.VK.AccessToken == "" {
		return fmt.Errorf("VK_ACCESS_TOKEN is required")
	}
	if len(c.Manual.UserIDs) > 0 {
		for _, id := range c.Manual.UserIDs {
			if id <= 0 {
				return fmt.Errorf("MANUAL_TRIGGER_USER_IDS must contain only positive int64 values")
			}
		}
	}
	if len(c.Manual.UserIDs) > 0 && strings.TrimSpace(c.Manual.Command) == "" {
		return fmt.Errorf("MANUAL_TRIGGER_COMMAND must not be empty when manual trigger user IDs are set")
	}
	c.Manual.UserIDsSet = make(map[int64]struct{}, len(c.Manual.UserIDs))
	for _, id := range c.Manual.UserIDs {
		c.Manual.UserIDsSet[id] = struct{}{}
	}
	if c.Summary.BatchSize <= 0 {
		return fmt.Errorf("SUMMARY_BATCH_SIZE must be > 0")
	}
	if c.Summary.MaxContextChars <= 0 {
		return fmt.Errorf("SUMMARY_MAX_CONTEXT_CHARS must be > 0")
	}
	if c.Summary.MaxContextMessages <= 0 {
		return fmt.Errorf("SUMMARY_MAX_CONTEXT_MESSAGES must be > 0")
	}
	if c.Summary.MinMessageLength < 1 {
		return fmt.Errorf("SUMMARY_MIN_MESSAGE_LENGTH must be >= 1")
	}
	if err := validateLLMConfig("LLM", c.LLM); err != nil {
		return err
	}
	if c.Image.Enabled {
		if err := validateLLMConfig("SUMMARY_IMAGE_PROMPT_LLM", c.ImagePromptLLM); err != nil {
			return err
		}
		if c.Image.APIKey == "" {
			return fmt.Errorf("SUMMARY_IMAGE_API_KEY or LLM_API_KEY is required when SUMMARY_IMAGE_ENABLED=true")
		}
		switch c.Image.Provider {
		case "yandex_art", "":
			if c.Image.FolderID == "" {
				return fmt.Errorf("SUMMARY_IMAGE_FOLDER_ID is required when SUMMARY_IMAGE_PROVIDER=yandex_art")
			}
			if c.Image.PollInterval <= 0 {
				return fmt.Errorf("SUMMARY_IMAGE_POLL_INTERVAL must be > 0")
			}
			if c.Image.WidthRatio <= 0 || c.Image.HeightRatio <= 0 {
				return fmt.Errorf("SUMMARY_IMAGE_WIDTH_RATIO and SUMMARY_IMAGE_HEIGHT_RATIO must be > 0")
			}
		case "openai":
			if c.Image.Model == "" {
				return fmt.Errorf("SUMMARY_IMAGE_MODEL is required when SUMMARY_IMAGE_PROVIDER=openai")
			}
			switch c.Image.Quality {
			case "", "auto", "low", "medium", "high":
			default:
				return fmt.Errorf("SUMMARY_IMAGE_QUALITY must be one of auto, low, medium, high when SUMMARY_IMAGE_PROVIDER=openai")
			}
			if c.Image.Width <= 0 || c.Image.Height <= 0 {
				return fmt.Errorf("SUMMARY_IMAGE_WIDTH and SUMMARY_IMAGE_HEIGHT must be > 0")
			}
		case "cloudflare":
			if c.Image.AccountID == "" {
				return fmt.Errorf("SUMMARY_IMAGE_ACCOUNT_ID is required when SUMMARY_IMAGE_PROVIDER=cloudflare")
			}
			if c.Image.Model == "" {
				return fmt.Errorf("SUMMARY_IMAGE_MODEL is required when SUMMARY_IMAGE_PROVIDER=cloudflare")
			}
			if c.Image.Width <= 0 || c.Image.Height <= 0 {
				return fmt.Errorf("SUMMARY_IMAGE_WIDTH and SUMMARY_IMAGE_HEIGHT must be > 0")
			}
		default:
			return fmt.Errorf("unsupported SUMMARY_IMAGE_PROVIDER %q", c.Image.Provider)
		}
		if c.Image.Timeout <= 0 {
			return fmt.Errorf("SUMMARY_IMAGE_TIMEOUT must be > 0")
		}
	}
	return nil
}

func validateLLMConfig(prefix string, cfg LLMConfig) error {
	if cfg.Provider == "openai_compat" {
		if cfg.BaseURL == "" {
			return fmt.Errorf("%s_BASE_URL is required for openai_compat", prefix)
		}
		if cfg.APIKey == "" {
			return fmt.Errorf("%s_API_KEY is required for openai_compat", prefix)
		}
	}
	return nil
}

func defaultImagePromptLLMModel(main LLMConfig) string {
	if main.Provider == "openai_compat" && strings.Contains(main.BaseURL, "api.openai.com") {
		return "gpt-5.4-nano"
	}
	return main.Model
}

func (l *envLoader) getString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (l *envLoader) getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		l.setErr(fmt.Errorf("invalid int for %s: %w", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) getBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		l.setErr(fmt.Errorf("invalid bool for %s: %w", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) getInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		l.setErr(fmt.Errorf("invalid int64 for %s: %w", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) getFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		l.setErr(fmt.Errorf("invalid float for %s: %w", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) getDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		l.setErr(fmt.Errorf("invalid duration for %s: %w", key, err))
		return fallback
	}
	return parsed
}

func (l *envLoader) getInt64List(key string) []int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			l.setErr(fmt.Errorf("invalid int64 in %s: %w", key, err))
			return nil
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result
}

func folderIDFromModelURI(model string) string {
	if !strings.HasPrefix(model, "gpt://") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(model, "gpt://"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
