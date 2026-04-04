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

	VK      VKConfig
	Manual  ManualTriggerConfig
	Summary SummaryConfig
	LLM     LLMConfig
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
	UserIDs     []int64
	UserIDsSet  map[int64]struct{}
	Command     string
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

func Load() (Config, error) {
	cfg := Config{
		AppEnv:                 getString("APP_ENV", "dev"),
		LogLevel:               getString("LOG_LEVEL", "INFO"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		DatabaseMaxConns:       int32(getInt("DB_MAX_CONNS", 10)),
		DatabaseMinConns:       int32(getInt("DB_MIN_CONNS", 1)),
		DatabaseConnectTimeout: getDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
		DatabaseQueryTimeout:   getDuration("DB_QUERY_TIMEOUT", 5*time.Second),
		VK: VKConfig{
			GroupID:        getInt64("VK_GROUP_ID", 0),
			AccessToken:    os.Getenv("VK_ACCESS_TOKEN"),
			APIVersion:     getString("VK_API_VERSION", "5.199"),
			LongPollWait:   getInt("VK_LONGPOLL_WAIT", 25),
			RequestTimeout: getDuration("VK_REQUEST_TIMEOUT", 20*time.Second),
			SendRandomID:   getInt("VK_SEND_RANDOM_ID", 0),
		},
		Manual: ManualTriggerConfig{
			UserIDs: getInt64List("MANUAL_TRIGGER_USER_IDS"),
			Command: getString("MANUAL_TRIGGER_COMMAND", "/summary"),
		},
		Summary: SummaryConfig{
			BatchSize:          getInt("SUMMARY_BATCH_SIZE", 200),
			MaxContextChars:    getInt("SUMMARY_MAX_CONTEXT_CHARS", 12000),
			MaxContextMessages: getInt("SUMMARY_MAX_CONTEXT_MESSAGES", 200),
			MinMessageLength:   getInt("SUMMARY_MIN_MESSAGE_LENGTH", 3),
		},
		LLM: LLMConfig{
			Provider:        getString("LLM_PROVIDER", "stub"),
			RequestTimeout:  getDuration("LLM_REQUEST_TIMEOUT", 600*time.Second),
			MaxRetries:      getInt("LLM_MAX_RETRIES", 2),
			RetryBaseDelay:  getDuration("LLM_RETRY_BASE_DELAY", 2*time.Second),
			Model:           getString("LLM_MODEL", "stub-sarcasm-v1"),
			BaseURL:         strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/"),
			APIKey:          os.Getenv("LLM_API_KEY"),
			Temperature:     getFloat("LLM_TEMPERATURE", 0.3),
			MaxOutputTokens: getInt("LLM_MAX_OUTPUT_TOKENS", 220),
			PromptMaxChars:  getInt("LLM_PROMPT_MAX_CHARS", 12000),
		},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
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
	if c.LLM.Provider == "openai_compat" {
		if c.LLM.BaseURL == "" {
			return fmt.Errorf("LLM_BASE_URL is required for openai_compat")
		}
		if c.LLM.APIKey == "" {
			return fmt.Errorf("LLM_API_KEY is required for openai_compat")
		}
	}
	return nil
}

func getString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("invalid int for %s: %v", key, err))
	}
	return parsed
}

func getInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("invalid int64 for %s: %v", key, err))
	}
	return parsed
}

func getFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(fmt.Sprintf("invalid float for %s: %v", key, err))
	}
	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		panic(fmt.Sprintf("invalid duration for %s: %v", key, err))
	}
	return parsed
}

func getInt64List(key string) []int64 {
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
			panic(fmt.Sprintf("invalid int64 in %s: %v", key, err))
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result
}
