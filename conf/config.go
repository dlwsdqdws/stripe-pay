package conf

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	config     *Config
	configOnce sync.Once
)

type Config struct {
	Server struct {
		Port string `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"server"`

	Stripe struct {
		SecretKey     string `yaml:"secret_key"`
		WebhookSecret string `yaml:"webhook_secret"`
	} `yaml:"stripe"`

	Apple struct {
		SharedSecret  string `yaml:"shared_secret"`
		ProductionURL string `yaml:"production_url"`
		SandboxURL    string `yaml:"sandbox_url"`
	} `yaml:"apple"`

	Log struct {
		Level       string `yaml:"level"`       // debug, info, warn, error
		Environment string `yaml:"environment"` // development, production
		Output      string `yaml:"output"`      // console, json (生产环境推荐 json)
	} `yaml:"log"`

	Database struct {
		Host            string `yaml:"host"`
		Port            int    `yaml:"port"`
		User            string `yaml:"user"`
		Password        string `yaml:"password"`
		Database        string `yaml:"database"`
		MaxOpenConns    int    `yaml:"max_open_conns"`
		MaxIdleConns    int    `yaml:"max_idle_conns"`
		ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
	} `yaml:"database"`

	Redis struct {
		Address      string `yaml:"address"`
		Port         int    `yaml:"port"`
		Password     string `yaml:"password"`
		DB           int    `yaml:"db"`
		DialTimeout  int    `yaml:"dial_timeout"`  // 秒
		ReadTimeout  int    `yaml:"read_timeout"`  // 秒
		WriteTimeout int    `yaml:"write_timeout"` // 秒
		PoolSize     int    `yaml:"pool_size"`
		MinIdleConns int    `yaml:"min_idle_conns"`
	} `yaml:"redis"`
}

func Init() error {
	var err error
	configOnce.Do(func() {
		config = &Config{}

		// 读取配置文件
		data, readErr := os.ReadFile("config.yaml")
		if readErr != nil {
			// 如果文件不存在，使用默认配置
			defaultConfig()
			err = nil
			return
		}

		if err = yaml.Unmarshal(data, config); err != nil {
			return
		}

		// 从环境变量覆盖配置
		loadFromEnv()

		// 验证必要的配置
		if err = validateConfig(); err != nil {
			return
		}
	})
	return err
}

func defaultConfig() {
	config.Server.Port = "8080"
	config.Server.Host = "0.0.0.0"
	config.Log.Level = "info"
	config.Log.Environment = "development"
	config.Log.Output = "console"

	// Redis 默认配置
	config.Redis.Address = ""
	config.Redis.Port = 6379
	config.Redis.DB = 0
	config.Redis.DialTimeout = 5
	config.Redis.ReadTimeout = 3
	config.Redis.WriteTimeout = 3
	config.Redis.PoolSize = 10
	config.Redis.MinIdleConns = 5
}

func loadFromEnv() {
	if secretKey := os.Getenv("STRIPE_SECRET_KEY"); secretKey != "" {
		config.Stripe.SecretKey = secretKey
	}
	if webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET"); webhookSecret != "" {
		config.Stripe.WebhookSecret = webhookSecret
	}
	if sharedSecret := os.Getenv("APPLE_SHARED_SECRET"); sharedSecret != "" {
		config.Apple.SharedSecret = sharedSecret
	}
	if dbPassword := os.Getenv("DB_PASSWORD"); dbPassword != "" {
		config.Database.Password = dbPassword
	}
	if redisAddr := os.Getenv("REDIS_ADDRESS"); redisAddr != "" {
		config.Redis.Address = redisAddr
	}
	if redisPort := os.Getenv("REDIS_PORT"); redisPort != "" {
		if port, err := strconv.Atoi(redisPort); err == nil {
			config.Redis.Port = port
		}
	}
	if redisPassword := os.Getenv("REDIS_PASSWORD"); redisPassword != "" {
		config.Redis.Password = redisPassword
	}
	if redisDB := os.Getenv("REDIS_DB"); redisDB != "" {
		if db, err := strconv.Atoi(redisDB); err == nil {
			config.Redis.DB = db
		}
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.Log.Level = logLevel
	}
	if logEnv := os.Getenv("LOG_ENVIRONMENT"); logEnv != "" {
		config.Log.Environment = logEnv
	}
	if logOutput := os.Getenv("LOG_OUTPUT"); logOutput != "" {
		config.Log.Output = logOutput
	}
}

func validateConfig() error {
	if config.Stripe.SecretKey == "" {
		return fmt.Errorf("Stripe secret key is required")
	}
	return nil
}

func GetConf() *Config {
	if config == nil {
		panic("config not initialized")
	}
	return config
}
