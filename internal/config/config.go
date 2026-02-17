package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config centralizes runtime settings for the MVP agent pipeline.
type Config struct {
	HTTPAddr          string
	SQLiteDSN         string
	RequestTimeoutSec int

	OpenAIAPIKey  string
	OpenAIModel   string
	OpenAIBaseURL string

	CryptoPanicAPIKey string
	LunarCrushAPIKey  string

	ExchangeBaseURL   string
	ExchangeAPIKey    string
	ExchangeSecretKey string

	MaxSingleStakeUSDT float64 // 单笔最大下单金额上限
	MaxDailyLossUSDT   float64
	MaxExposureUSDT    float64
	MinConfidence      float64

	DryRun bool

	// 交易模式: "spot"（现货）或 "futures"（永续合约）
	TradingMode       string
	FuturesBaseURL    string
	FuturesLeverage   int
	FuturesMarginType string // "CROSSED" 或 "ISOLATED"

	// 定时任务
	AutoRunEnabled  bool
	AutoRunInterval int // 秒
	AutoRunPairs    string

	// OAuth 配置
	OAuthStoragePath string

	// LLM 认证配置
	LLMAuthMode     string // "api_key", "oauth", "auto"（默认）
	LLMAuthProvider string // "openai", "anthropic"（默认 openai）
}

func Load() Config {
	// Auto-load .env file if present (won't override existing env vars)
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using system environment variables")
	}

	return Config{
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		SQLiteDSN:         getEnv("SQLITE_DSN", "file:./ai_quant.db?_pragma=busy_timeout(5000)"),
		RequestTimeoutSec: getEnvInt("REQUEST_TIMEOUT_SEC", 15),

		OpenAIAPIKey:  getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:   getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL: getEnv("OPENAI_BASE_URL", ""),

		CryptoPanicAPIKey: getEnv("CRYPTOPANIC_API_KEY", ""),
		LunarCrushAPIKey:  getEnv("LUNARCRUSH_API_KEY", ""),

		ExchangeBaseURL:   getEnv("EXCHANGE_BASE_URL", "https://api.binance.com"),
		ExchangeAPIKey:    getEnv("EXCHANGE_API_KEY", ""),
		ExchangeSecretKey: getEnv("EXCHANGE_SECRET_KEY", ""),

		MaxSingleStakeUSDT: getEnvFloatWithFallback("MAX_SINGLE_STAKE_USDT", "DEFAULT_STAKE_USDT", 50),
		MaxDailyLossUSDT:   getEnvFloat("MAX_DAILY_LOSS_USDT", 100),
		MaxExposureUSDT:    getEnvFloat("MAX_EXPOSURE_USDT", 200),
		MinConfidence:      getEnvFloat("MIN_CONFIDENCE", 0.55),

		DryRun: getEnvBool("DRY_RUN", true),

		TradingMode:       getEnv("TRADING_MODE", "spot"),
		FuturesBaseURL:    getEnv("FUTURES_BASE_URL", "https://fapi.binance.com"),
		FuturesLeverage:   getEnvInt("FUTURES_LEVERAGE", 3),
		FuturesMarginType: getEnv("FUTURES_MARGIN_TYPE", "CROSSED"),

		AutoRunEnabled:  getEnvBool("AUTO_RUN_ENABLED", false),
		AutoRunInterval: getEnvInt("AUTO_RUN_INTERVAL_SEC", 60),
		AutoRunPairs:    getEnv("AUTO_RUN_PAIRS", "BTC/USDT"),

		OAuthStoragePath: getEnv("OAUTH_STORAGE_PATH", ""),

		LLMAuthMode:     getEnv("LLM_AUTH_MODE", "auto"),
		LLMAuthProvider: getEnv("LLM_AUTH_PROVIDER", "openai"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

// getEnvFloatWithFallback 优先读取新变量名，如果不存在则尝试旧变量名（向后兼容）
func getEnvFloatWithFallback(newKey, oldKey string, fallback float64) float64 {
	if v := os.Getenv(newKey); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return parsed
		}
	}
	// 尝试读取旧变量名
	if v := os.Getenv(oldKey); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
