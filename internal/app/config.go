package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL, Address, Origin string
	SecureCookie                 bool
}

func LoadConfig() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Address: os.Getenv("HTTP_ADDR"), Origin: os.Getenv("PUBLIC_ORIGIN")}
	if c.Address == "" {
		c.Address = "127.0.0.1:8080"
	}
	if c.Origin == "" {
		c.Origin = "http://localhost:5173"
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return c, fmt.Errorf("PUBLIC_ORIGIN must be an HTTP(S) origin without a path")
	}
	c.SecureCookie = u.Scheme == "https"
	if os.Getenv("APP_ENV") == "production" && !c.SecureCookie {
		return c, fmt.Errorf("production requires HTTPS PUBLIC_ORIGIN")
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func envInt(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envString(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
