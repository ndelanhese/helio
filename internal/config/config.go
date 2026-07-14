package config

import "os"

type Config struct{ HTTPAddr string }

func Load() Config {
	addr := os.Getenv("HELIO_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return Config{HTTPAddr: addr}
}
