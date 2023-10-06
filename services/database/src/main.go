package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"pulsecore/services/database/src/dbutils"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

var (
	db               *pgx.Conn
	allowedTablesMap map[string]bool
)

func main() {

	var err error
	file, err := os.Open("../config.json")
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var config dbutils.DBConfig
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Failed to decode config: %v", err)
	}

	allowedTablesMap = make(map[string]bool)
	for _, table := range config.AllowedTables {
		allowedTablesMap[table] = true
	}

	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", config.Username, config.Password, config.Hostname, config.Port, config.DBName)
	db, err = pgx.Connect(context.Background(), connString)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close(context.Background())

	r := gin.Default()

	r.GET("/database/records", fetchRecords)
	r.POST("/database/records", createRecords)
	r.PUT("/database/records", updateRecords)
	r.DELETE("/database/records", deleteRecords)
	r.GET("/database/health", checkHealth)

	r.Run(":8091")
}

func fetchRecords(c *gin.Context) {
	var requestData struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, ok := allowedTablesMap[requestData.TableName]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid table name provided"})
		return
	}

	selectColumns := "*"
	if len(requestData.Columns) > 0 {
		selectColumns = strings.Join(requestData.Columns, ", ")
	}

	whereClauses := make([]string, 0)
	values := make([]interface{}, 0)

	var i = 1
	for column, value := range requestData.Conditions {
		whereClauses = append(whereClauses, fmt.Sprintf("%s=$%d", column, i))
		values = append(values, value)
		i++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = fmt.Sprintf("WHERE %s", strings.Join(whereClauses, " AND "))
	}

	query := fmt.Sprintf("SELECT %s FROM %s %s", selectColumns, requestData.TableName, whereClause)

	rows, err := db.Query(context.Background(), query, values...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var allRecords []map[string]interface{}

	fieldDescriptions := rows.FieldDescriptions()

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		singleRecord := make(map[string]interface{})

		for i, fd := range fieldDescriptions {
			singleRecord[string(fd.Name)] = values[i]
		}

		allRecords = append(allRecords, singleRecord)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, allRecords)
}

func createRecords(c *gin.Context) {
	var inputData struct {
		TableName string                 `json:"table_name"`
		Record    map[string]interface{} `json:"record"`
	}

	if err := c.ShouldBindJSON(&inputData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, ok := allowedTablesMap[inputData.TableName]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid table name provided"})
		return
	}

	if err := dbutils.InsertRecord(db, inputData.TableName, inputData.Record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Record created successfully"})
}

func updateRecords(c *gin.Context) {
	var inputData struct {
		ID        string                 `json:"id"`
		TableName string                 `json:"table_name"`
		Fields    map[string]interface{} `json:"fields"`
	}

	if err := c.ShouldBindJSON(&inputData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tableName := inputData.TableName
	if _, ok := allowedTablesMap[tableName]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid table name provided"})
		return
	}

	setClauses := make([]string, 0, len(inputData.Fields))
	values := make([]interface{}, 0, len(inputData.Fields)+1)

	var i = 1
	for column, value := range inputData.Fields {
		setClauses = append(setClauses, fmt.Sprintf("%s=$%d", column, i))
		values = append(values, value)
		i++
	}

	values = append(values, inputData.ID)
	updateQuery := fmt.Sprintf("UPDATE %s SET %s WHERE id=$%d", tableName, strings.Join(setClauses, ", "), i)

	_, err := db.Exec(context.Background(), updateQuery, values...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Record with ID %s in table %s updated successfully", inputData.ID, tableName)})
}

func deleteRecords(c *gin.Context) {
	var inputData struct {
		ID        string `json:"id"`
		TableName string `json:"table_name"`
	}

	if err := c.ShouldBindJSON(&inputData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tableName := inputData.TableName
	if _, ok := allowedTablesMap[tableName]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid table name provided"})
		return
	}

	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE id=$1", tableName)
	_, err := db.Exec(context.Background(), deleteQuery, inputData.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Record with ID %s in table %s deleted successfully", inputData.ID, tableName)})
}

func checkHealth(c *gin.Context) {
	err := db.Ping(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "Unhealthy", "message": "Database connection failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "Healthy", "message": "Database service is running."})
}
