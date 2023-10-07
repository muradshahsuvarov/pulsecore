package dbutils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v4/pgxpool"
)

type DBConfig struct {
	Hostname      string                 `json:"hostname"`
	Port          string                 `json:"port"`
	Username      string                 `json:"username"`
	Password      string                 `json:"password"`
	DBName        string                 `json:"dbname"`
	AllowedTables map[string]TableConfig `json:"allowedTables"`
}

type TableConfig struct {
	NonUpdatableColumns []string `json:"nonUpdatableColumns"`
}

var config DBConfig

func init() {
	config, _ = LoadConfig("../config.json")
}

func InsertRecord(db *pgxpool.Pool, tableName string, record map[string]interface{}) error {

	columns := make([]string, len(record))
	values := make([]interface{}, len(record))

	i := 0
	for column, value := range record {
		columns[i] = column
		values[i] = value
		i++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName, strings.Join(columns, ", "), PlaceHolders(len(columns)))

	_, err := db.Exec(context.Background(), query, values...)
	if err != nil {
		return err
	}

	return nil
}

func PlaceHolders(count int) string {
	placeholders := make([]string, count)
	for i := 1; i <= count; i++ {
		placeholders[i-1] = fmt.Sprintf("$%d", i)
	}
	return strings.Join(placeholders, ", ")
}

func LoadConfig(path string) (DBConfig, error) {
	var config DBConfig
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	return config, err
}

func NonUpdatableColumns(tableName string) []string {
	tableConfig, ok := config.AllowedTables[tableName]
	if !ok {
		return nil
	}
	return tableConfig.NonUpdatableColumns
}
