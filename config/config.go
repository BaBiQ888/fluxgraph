package config

import (
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Config struct {
	App           AppConfig           `mapstructure:"app"`
	Storage       StorageConfig       `mapstructure:"storage"`
	LLM           LLMConfig           `mapstructure:"llm"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

type AppConfig struct {
	Name      string `mapstructure:"name" validate:"required"`
	Env       string `mapstructure:"env" validate:"oneof=development production test"`
	Port      int    `mapstructure:"port" validate:"required"`
	GRPCPort  int    `mapstructure:"grpc_port"`
	A2ASecret string `mapstructure:"a2a_secret" validate:"required"`
}

type StorageConfig struct {
	Redis    RedisConfig    `mapstructure:"redis"`
	Postgres PostgresConfig `mapstructure:"postgres"`
}

type RedisConfig struct {
	URL      string        `mapstructure:"url" validate:"required"`
	PoolSize int           `mapstructure:"pool_size"`
	TTL      time.Duration `mapstructure:"ttl"`
}

type PostgresConfig struct {
	URL      string `mapstructure:"url" validate:"required"`
	MaxConns int    `mapstructure:"max_conns"`
}

type LLMConfig struct {
	OpenAI     ProviderConfig   `mapstructure:"openai"`
	Anthropic  ProviderConfig   `mapstructure:"anthropic"`
	Resiliency ResiliencyConfig `mapstructure:"resiliency"`
}

type ProviderConfig struct {
	APIKey  string        `mapstructure:"api_key"`
	BaseURL string        `mapstructure:"base_url"`
	Model   string        `mapstructure:"model"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type ResiliencyConfig struct {
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

type CircuitBreakerConfig struct {
	MaxFailures int           `mapstructure:"max_failures"`
	Interval    time.Duration `mapstructure:"interval"`
	Timeout     time.Duration `mapstructure:"timeout"`
}

type ObservabilityConfig struct {
	OtelEndpoint   string `mapstructure:"otel_endpoint"`
	PrometheusPort int    `mapstructure:"prometheus_port"`
}

func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	// Default values
	v.SetDefault("app.name", "fluxgraph")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.port", 8080)
	v.SetDefault("app.grpc_port", 50051)
	v.SetDefault("storage.redis.pool_size", 10)
	v.SetDefault("storage.redis.ttl", 24*time.Hour)
	v.SetDefault("storage.postgres.max_conns", 20)
	v.SetDefault("observability.prometheus_port", 9090)
	v.SetDefault("llm.resiliency.circuit_breaker.max_failures", 5)
	v.SetDefault("llm.resiliency.circuit_breaker.interval", 60*time.Second)
	v.SetDefault("llm.resiliency.circuit_breaker.timeout", 30*time.Second)

	// Environment variables
	v.SetEnvPrefix("FLUXGRAPH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Validate required fields
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
