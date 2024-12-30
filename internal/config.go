package internal

import (
	"github.com/ilyakaznacheev/cleanenv"
	"log"
	"os"
	"time"
)

type Config struct {
	Env        string `yaml:"env" env:"ENV" env-default:"prod" env-required:"true"`
	Redis      string `yaml:"redis_conn" env:"REDIS_CONN" env-required:"true"`
	Postgres   string `yaml:"postgres_conn" env:"PG_CONN" env-required:"true"`
	HTTPServer `yaml:"http_server"`
}
type HTTPServer struct {
	Addr        string        `yaml:"address" env-default:"localhost:8080"`
	ReqTimeout  time.Duration `yaml:"req_timeout" env-default:"4s"`
	IdleTimeout time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

func MustLoad() *Config {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH environment variable not set")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("config file does not exist: %s", configPath)
	}

	var cfg Config
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("can't read config: %s", err)
	}

	return &cfg
}
