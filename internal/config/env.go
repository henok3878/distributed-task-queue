package config

import (
	"fmt"
	"os"
	"strings"
)

func GetFromEnv(key string) (string, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", fmt.Errorf("required env %s is not set", key)
	}
	return v, nil
}
