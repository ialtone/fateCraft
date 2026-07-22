package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config 保存来自环境变量的运行时配置。
type Config struct {
	Port           string
	GinMode        string
	DatabaseURL    string
	DataDir        string   // 判定配置、响应 Schema 和空库首次初始化材料的根目录
	LLMAPIKeyEnv   string   // 当数据库中的 api_key 为空时使用的兜底密钥
	AllowedOrigins []string // 允许的 CORS/WebSocket 来源；为空表示全放开（仅开发用）
	LogLevel       string   // 日志级别：debug/info/warn/error（默认 info）
	LogFormat      string   // 日志格式：console/json（默认 console，生产建议 json）

	// 认证策略由数据库和管理后台维护；环境变量只承载部署密钥与 OAuth 凭据。
	LinuxDOClientID       string
	LinuxDOClientSecret   string
	LinuxDORedirectURI    string
	AuthDataEncryptionKey string
	AuthSessionTTL        time.Duration
	AuthCookieSecure      bool
}

// Load 从环境变量读取配置，存在 .env 文件时会一并加载。
func Load() Config {
	// 尽力加载：没有 .env 文件时忽略错误。
	_ = godotenv.Load()

	return Config{
		Port:                  getenv("PORT", "8081"),
		GinMode:               getenv("GIN_MODE", "debug"),
		DatabaseURL:           getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fatecraft?sslmode=disable"),
		DataDir:               getenv("DATA_DIR", "data"),
		LLMAPIKeyEnv:          os.Getenv("LLM_API_KEY"),
		AllowedOrigins:        getlist("ALLOWED_ORIGINS"),
		LogLevel:              getenv("LOG_LEVEL", "info"),
		LogFormat:             getenv("LOG_FORMAT", "console"),
		LinuxDOClientID:       os.Getenv("LINUXDO_CLIENT_ID"),
		LinuxDOClientSecret:   os.Getenv("LINUXDO_CLIENT_SECRET"),
		LinuxDORedirectURI:    os.Getenv("LINUXDO_REDIRECT_URI"),
		AuthDataEncryptionKey: os.Getenv("AUTH_DATA_ENCRYPTION_KEY"),
		AuthSessionTTL:        getduration("AUTH_SESSION_TTL", 14*24*time.Hour),
		AuthCookieSecure:      getbool("AUTH_COOKIE_SECURE", false),
	}
}

// getlist 读取逗号分隔的环境变量为字符串列表，空值返回 nil。
func getlist(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getbool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getduration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
