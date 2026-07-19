package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config 保存来自环境变量的运行时配置。
type Config struct {
	Port           string
	GinMode        string
	DevPlayerID    string
	DatabaseURL    string
	AutoMigrate    bool
	DataDir        string   // 模板 JSON 文件的数据目录
	LLMAPIKeyEnv   string   // 当数据库中的 api_key 为空时使用的兜底密钥
	AllowedOrigins []string // 允许的 CORS/WebSocket 来源；为空表示全放开（仅开发用）
	LogLevel       string   // 日志级别：debug/info/warn/error（默认 info）
	LogFormat      string   // 日志格式：console/json（默认 console，生产建议 json）
}

// Load 从环境变量读取配置，存在 .env 文件时会一并加载。
func Load() Config {
	// 尽力加载：没有 .env 文件时忽略错误。
	_ = godotenv.Load()

	return Config{
		Port:           getenv("PORT", "8081"),
		GinMode:        getenv("GIN_MODE", "debug"),
		DevPlayerID:    getenv("DEV_PLAYER_ID", "dev"),
		DatabaseURL:    getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/fatecraft?sslmode=disable"),
		AutoMigrate:    getbool("AUTO_MIGRATE", false),
		DataDir:        getenv("DATA_DIR", "data"),
		LLMAPIKeyEnv:   os.Getenv("LLM_API_KEY"),
		AllowedOrigins: getlist("ALLOWED_ORIGINS"),
		LogLevel:       getenv("LOG_LEVEL", "info"),
		LogFormat:      getenv("LOG_FORMAT", "console"),
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
