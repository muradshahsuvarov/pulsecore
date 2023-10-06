package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"pulsecore/services/middleware/src/mwutils"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	db              *pgxpool.Pool
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
	r.GET("/middleware/authenticated", isAuthenticated)
	r.POST("/middleware/application", createApplication)
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

	resp, err := http.Post(databaseService+"/query", "application/json", bytes.NewBuffer(data))
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

	c.JSON(http.StatusOK, gin.H{"token": token})
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

	resp, err := http.Post(databaseService, "application/json", strings.NewReader(fmt.Sprintf(`{
		"table_name": "users",
		"records": [{
			"email": "%s",
			"password": "%s"
		}]
	}`, request.Email, hashedPassword)))

	if err != nil || resp.StatusCode != http.StatusCreated {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error registering the user."})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "User registered successfully."})
}

func isAuthenticated(c *gin.Context) {

	authHeader := c.Request.Header.Get("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": "No Authorization header provided."})
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": "Authorization header format must be Bearer {token}"})
		return
	}
	tokenString := parts[1]

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if jwt.GetSigningMethod("HS256") != token.Method {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secretKey), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "not authenticated", "message": "Invalid token."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "authenticated", "message": "Valid token."})
}

func createApplication(c *gin.Context) {
	// Parse the incoming request for application details
	// Generate a unique AppID
	// Store the application details and AppID in the database
	// Return the AppID to the client
}

func checkHealth(c *gin.Context) {
	// Check the health of the database and other related services
	// Return the health status
}

func checkDatabaseHealthMiddleware(c *gin.Context) {
	err := mwutils.CheckDatabaseHealth(databaseService)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		c.Abort() // Important: This stops any further handlers from being executed
		return
	}
	c.Next() // Move on to the next middleware or handler
}
