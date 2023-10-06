package mwutils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dgrijalva/jwt-go"
)

type Config struct {
	SECRET_KEY       string `json:"SECRET_KEY"`
	DATABASE_SERVICE string `json:"DATABASE_SERVICE"`
}

func CreateToken(email string, secretKey string) (string, error) {
	claims := jwt.MapClaims{
		"email": email,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secretKey))
}

func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open config file: %v", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode config: %v", err)
	}

	return &config, nil
}

func CheckDatabaseHealth(databaseService string) error {
	resp, err := http.Get(databaseService + "/database/health")
	if err != nil {
		return fmt.Errorf("Failed to contact database service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var respError struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&respError); err != nil {
			return fmt.Errorf("Database service not healthy and failed to parse the error message.")
		}
		return fmt.Errorf("Database service not healthy: %s", respError.Message)
	}
	return nil
}
