package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"pulsecore/services/middleware/src/mwutils"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var (
	secretKey       = ""
	databaseService = ""
)

func main() {

	r := gin.Default()

	config, err := mwutils.LoadConfig("../config.json")

	if err != nil {
		fmt.Println("Error loading config:", err)
		return
	}

	secretKey = config.SECRET_KEY
	databaseService = config.DATABASE_SERVICE

	r.Use(checkDatabaseHealthMiddleware)

	r.POST("/middleware/authenticate", authenticateUser)
	r.POST("/middleware/register", registerUser)
	r.GET("/middleware/authenticated", checkAuthentication)
	r.POST("/middleware/application", createApplication)
	r.PUT("/middleware/application", updateApplication)
	r.DELETE("/middleware/application", deleteApplication)
	r.POST("/middleware/server", createServer)
	r.DELETE("/middleware/server", deleteServer)
	r.GET("/middleware/health", checkHealth)

	r.Run(":8093")
}

func authenticateUser(c *gin.Context) {

	err := mwutils.CheckDatabaseHealth(databaseService)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	payload := struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}{
		TableName: "users",
		Conditions: map[string]interface{}{
			"email": request.Email,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	resp, err := http.Post(databaseService+"/records/query", "application/json", bytes.NewBuffer(data))
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching user data."})
		return
	}

	var users []struct {
		ID       int    `json:"id"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil || len(users) != 1 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error parsing user data."})
		return
	}

	user := users[0]

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password."})
		return
	}

	token, err := mwutils.CreateToken(user.Email, secretKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create token."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user_id": user.ID})
}

func registerUser(c *gin.Context) {

	err := mwutils.CheckDatabaseHealth(databaseService)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not hash password."})
		return
	}

	resp, err := http.Post(databaseService+"/records", "application/json", strings.NewReader(fmt.Sprintf(`{
		"table_name": "users",
		"records": [{
			"email": "%s",
			"password": "%s"
		}]
	}`, request.Email, hashedPassword)))

	fmt.Println("resp.StatusCode:    ", resp.StatusCode)

	if err != nil || resp.StatusCode != http.StatusCreated {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error registering the user."})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User registered successfully."})
}

func isAuthenticated(c *gin.Context) (bool, error) {
	authHeader := c.Request.Header.Get("Authorization")
	if authHeader == "" {
		return false, fmt.Errorf("No Authorization header provided.")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return false, fmt.Errorf("Authorization header format must be Bearer {token}")
	}
	tokenString := parts[1]

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if jwt.GetSigningMethod("HS256") != token.Method {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secretKey), nil
	})

	if err != nil || !token.Valid {
		return false, fmt.Errorf("Invalid token.")
	}

	return true, nil
}

func checkAuthentication(c *gin.Context) {
	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "authenticated", "message": "Valid token."})
}

func createApplication(c *gin.Context) {
	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}

	var request struct {
		AppName        string `json:"app_name"`
		AppDescription string `json:"app_description"`
		UserID         int    `json:"user_id"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appIdentifier := mwutils.GenerateUUID()

	data, err := json.Marshal(map[string]interface{}{
		"table_name": "applications",
		"records": []map[string]interface{}{
			{
				"app_name":        request.AppName,
				"app_identifier":  appIdentifier,
				"app_description": request.AppDescription,
				"user_id":         request.UserID,
				"date_created":    time.Now(),
				"last_updated":    time.Now(),
			},
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	resp, err := http.Post(databaseService+"/records", "application/json", bytes.NewBuffer(data))
	if err != nil || resp.StatusCode != http.StatusCreated {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error saving the application."})
		return
	}

	c.JSON(http.StatusCreated, map[string]interface{}{
		"app_name":        request.AppName,
		"app_identifier":  appIdentifier,
		"app_description": request.AppDescription,
		"user_id":         request.UserID,
		"date_created":    time.Now(),
		"last_updated":    time.Now(),
	})
}

func updateApplication(c *gin.Context) {
	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}

	var request struct {
		AppID          string `json:"app_id"`
		AppName        string `json:"app_name,omitempty"`
		AppDescription string `json:"app_description,omitempty"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updateFields := make(map[string]interface{})

	if request.AppName != "" {
		updateFields["app_name"] = request.AppName
	}

	if request.AppDescription != "" {
		updateFields["app_description"] = request.AppDescription
	}

	updateFields["last_updated"] = time.Now()

	data, err := json.Marshal([]map[string]interface{}{
		{
			"ids":        []string{request.AppID},
			"table_name": "applications",
			"fields":     updateFields,
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	req, err := http.NewRequest("PUT", databaseService+"/records", bytes.NewBuffer(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating the PUT request."})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error sending the PUT request."})
		return
	}

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error updating the application."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Application updated successfully."})
}

func deleteApplication(c *gin.Context) {
	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}

	var request struct {
		AppID string `json:"app_id"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, err := json.Marshal(map[string]interface{}{
		"table_name": "server_addresses",
		"conditions": map[string]interface{}{
			"app_id": request.AppID,
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	req, err := http.NewRequest("DELETE", databaseService+"/records", bytes.NewBuffer(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating the DELETE request for servers."})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting associated servers."})
		return
	}

	data, err = json.Marshal(map[string]interface{}{
		"table_name": "applications",
		"conditions": map[string]interface{}{
			"id": request.AppID,
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	req, err = http.NewRequest("DELETE", databaseService+"/records", bytes.NewBuffer(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating the DELETE request for application."})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting the application."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Application and its associated servers deleted successfully based on app_id."})
}

func createServer(c *gin.Context) {

	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}

	var request struct {
		Address string `json:"address"`
		AppID   string `json:"app_identifier"`
		Tag     string `json:"tag"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	serverData, err := json.Marshal(map[string]interface{}{
		"table_name": "server_addresses",
		"records": []map[string]interface{}{
			{
				"address":        request.Address,
				"app_identifier": request.AppID,
				"tag":            request.Tag,
				"date_added":     time.Now().Format(time.RFC3339), // Ensuring the date is in a standardized format
			},
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating server request payload."})
		return
	}

	resp, err := http.Post(databaseService+"/records", "application/json", bytes.NewBuffer(serverData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error sending data to the database service: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database service error: " + string(bodyBytes)})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Server created successfully."})
}

func checkHealth(c *gin.Context) {
	resp, err := http.Get(databaseService + "/health")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "down", "message": "Unable to connect to the database service."})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResponse map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "down", "message": "Error parsing response from database service."})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "down", "message": errorResponse["error"]})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "up", "message": "Middleware and associated services are operational."})
}

func deleteServer(c *gin.Context) {

	isAuth, err := isAuthenticated(c)
	if !isAuth {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": err.Error()})
		return
	}

	var request struct {
		ServerID int `json:"server_id"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, err := json.Marshal(map[string]interface{}{
		"table_name": "server_addresses",
		"conditions": map[string]interface{}{
			"id": request.ServerID,
		},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating request payload."})
		return
	}

	req, err := http.NewRequest("DELETE", databaseService+"/records", bytes.NewBuffer(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error creating the DELETE request."})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting the server."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Server deleted successfully."})
}

func checkDatabaseHealthMiddleware(c *gin.Context) {
	err := mwutils.CheckDatabaseHealth(databaseService)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		c.Abort()
		return
	}
	c.Next()
}
