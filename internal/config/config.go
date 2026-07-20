package config

import (
	"encoding/base64"
	"os"
)

type Config struct {
	HTTPAddr          string
	DatabasePath      string
	SecureCookies     bool
	AllowPublicLogger bool
	SecretsKey        []byte
}

func Load() Config {
	addr := os.Getenv("HELIO_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	databasePath := os.Getenv("HELIO_DATABASE_PATH")
	if databasePath == "" {
		databasePath = "/data/helio.db"
	}
	var secretsKey []byte
	if encoded := os.Getenv("HELIO_SECRETS_KEY"); encoded != "" {
		secretsKey, _ = base64.StdEncoding.DecodeString(encoded)
	}
	return Config{HTTPAddr: addr, DatabasePath: databasePath,
		SecureCookies:     os.Getenv("HELIO_SECURE_COOKIES") == "1",
		AllowPublicLogger: os.Getenv("HELIO_ALLOW_NON_PRIVATE_LOGGER") == "1",
		SecretsKey:        secretsKey}
}
