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
	"github.com/jackc/pgx/v4/pgxpool"
)

var (
	db               *pgxpool.Pool
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
	for table := range config.AllowedTables {
		allowedTablesMap[table] = true
	}

	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", config.Username, config.Password, config.Hostname, config.Port, config.DBName)
	db, err = pgxpool.Connect(context.Background(), connString)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	r := gin.Default()

	r.POST("/database/records/query", fetchRecords)
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
	conn, err := db.Acquire(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to acquire a database connection"})
		return
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), query, values...)
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
		TableName string                   `json:"table_name"`
		Records   []map[string]interface{} `json:"records"`
	}

	if err := c.ShouldBindJSON(&inputData); err != nil {
		log.Printf("Failed to bind JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, ok := allowedTablesMap[inputData.TableName]
	if !ok {
		log.Printf("Invalid table name: %s", inputData.TableName)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid table name provided"})
		return
	}

	for _, record := range inputData.Records {
		if err := dbutils.InsertRecord(db, inputData.TableName, record); err != nil {
			log.Printf("Error while inserting record: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to insert record: %v", err)})
			return
		}
	}

	c.JSON(http.StatusCreated, gin.H{"message": fmt.Sprintf("%d record(s) created successfully", len(inputData.Records))})
}

func updateRecords(c *gin.Context) {
	type UpdateInput struct {
		IDs       []string               `json:"ids"`
		TableName string                 `json:"table_name"`
		Fields    map[string]interface{} `json:"fields"`
	}

	var inputData []UpdateInput

	if err := c.ShouldBindJSON(&inputData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, record := range inputData {
		tableName := record.TableName
		if _, ok := allowedTablesMap[tableName]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid table name provided: %s", tableName)})
			return
		}

		nonUpdatableCols := dbutils.NonUpdatableColumns(tableName)
		for _, col := range nonUpdatableCols {
			delete(record.Fields, col)
		}

		setClauses := make([]string, 0, len(record.Fields))
		values := make([]interface{}, 0, len(record.Fields)+len(record.IDs))

		var i = 1
		for column, value := range record.Fields {
			setClauses = append(setClauses, fmt.Sprintf("%s=$%d", column, i))
			values = append(values, value)
			i++
		}

		placeholders := make([]string, len(record.IDs))
		for idx, id := range record.IDs {
			placeholders[idx] = fmt.Sprintf("$%d", i)
			values = append(values, id)
			i++
		}

		updateQuery := fmt.Sprintf("UPDATE %s SET %s WHERE id IN (%s)", tableName, strings.Join(setClauses, ", "), strings.Join(placeholders, ", "))

		conn, err := db.Acquire(context.Background())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err})
			return
		}
		defer conn.Release()

		_, err = conn.Exec(context.Background(), updateQuery, values...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Records updated successfully"})
}

func deleteRecords(c *gin.Context) {
	var inputData struct {
		TableName  string                 `json:"table_name"`
		Conditions map[string]interface{} `json:"conditions"`
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

	whereClauses := make([]string, 0)
	values := make([]interface{}, 0)

	var i = 1
	for column, value := range inputData.Conditions {
		whereClauses = append(whereClauses, fmt.Sprintf("%s=$%d", column, i))
		values = append(values, value)
		i++
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = fmt.Sprintf("WHERE %s", strings.Join(whereClauses, " AND "))
	}

	deleteQuery := fmt.Sprintf("DELETE FROM %s %s", tableName, whereClause)

	conn, err := db.Acquire(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
	}
	defer conn.Release()

	commandTag, err := conn.Exec(context.Background(), deleteQuery, values...)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	affectedRows := commandTag.RowsAffected()

	if affectedRows > 0 {
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("%d records in table %s deleted successfully based on conditions", affectedRows, tableName)})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"message": fmt.Sprintf("No records in table %s matched the provided conditions.", tableName)})
	}
}

func checkHealth(c *gin.Context) {
	err := db.Ping(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "Unhealthy", "message": "Database connection failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "Healthy", "message": "Database service is running."})
}
