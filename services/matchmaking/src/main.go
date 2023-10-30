package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

var rdb *redis.Client = &redis.Client{}

func main() {

	// Initiallizing redis connection
	defer rdb.Close()

	var redisAddr string

	flag.StringVar(&redisAddr, "redis-server", "", "Specify your redis address.\nOn local machine it is usually localhost:6379")
	flag.Parse()

	if redisAddr == "" {
		log.Fatal("Redis server address required for room management within the server!\n",
			"On local machines it is usually localhost:6379")
	}

	rdb = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})

	if rdb.Get(context.Background(), "room_id").Err() == redis.Nil {
		rdb.Set(context.Background(), "room_id", 0, 0)
	}

	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Unable to connect to Redis: %v", err)
	}
	fmt.Println("Connected to redis...")
	fmt.Println(strings.Repeat("=", 50))

	r := gin.Default()

	// Health check endpoint
	r.GET("/matchmaking/health", checkHealth)

	// Get a list of available rooms
	r.GET("/matchmaking/rooms", listAvailableRooms)

	// Create a new game room
	r.POST("/matchmaking/create-room", createRoom)

	// Join a specific room
	r.POST("/matchmaking/join-room", joinRoom)

	// Leave a specific room
	r.DELETE("/matchmaking/leave-room", leaveRoom)

	// List server specific rooms
	r.GET("/matchmaking/listrooms", listRooms)

	// Print room data
	r.GET("/matchmaking/roomdetails/:roomId", roomDetails)

	// Transfer host from the current host to a new one
	r.POST("/matchmaking/transfer-host", transferHost)

	r.Run(":8096")
}

func checkHealth(c *gin.Context) {
	// TODO: Add checks for database connections, third-party services, etc. here

	// If everything is okay
	c.JSON(200, gin.H{
		"status":  "Healthy",
		"message": "Matchmaking service is running.",
	})
}

func listAvailableRooms(c *gin.Context) {
	// WebSocket (TCP) based
	// List available game rooms for players to join
	// This data might come from a database or in-memory storage

	rooms := []string{"Room1", "Room2"} // Sample data
	c.JSON(200, rooms)
}

func createRoom(c *gin.Context) {

	var requestData struct {
		RoomName      string `json:"roomName"`
		ServerAddress string `json:"serverAddress"`
		RpcAddress    string `json:"rpcAddrerss"`
		ClientName    string `json:"clientName"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var roomName string = requestData.RoomName
	var serverAddr string = requestData.ServerAddress
	var rpcAddress string = requestData.RpcAddress
	var clientName string = requestData.ClientName

	// Check if a room with the given name already exists
	existingRoomKey := rdb.Keys(context.Background(), fmt.Sprintf("room_name:%s", roomName)).Val()
	if len(existingRoomKey) > 0 {
		var message string = fmt.Sprintf("A room with name %s already exists", roomName)
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}

	// Increment the room_id to get a new room ID
	roomID := rdb.Incr(context.Background(), "room_id").Val()

	roomKey := fmt.Sprintf("room:%d", roomID)
	err := rdb.HMSet(context.Background(), roomKey,
		"room_id", roomID,
		"room_name", roomName,
		"max_players", "10",
		"current_players", 1,
		"server_id", serverAddr,
		"status", "available",
		"properties", "{}",
		"host_rpc_address", rpcAddress+";"+clientName,
		"date_created", time.Now().Format(time.RFC3339),
	).Err()

	if err != nil {
		currentValue := rdb.Get(context.Background(), "room_id").Val()
		if currentValue != "0" {
			rdb.Decr(context.Background(), "room_id")
		}

		var message string = fmt.Sprintf("Error creating room: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": message})
		return
	}

	// Add the client name to the room's set of clients' names
	err = rdb.SAdd(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddress+";"+clientName).Err()
	if err != nil {
		var message string = fmt.Sprintf("Error adding client name to room: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": message})
		return
	}

	// Associate the room name with its key in Redis
	rdb.Set(context.Background(), fmt.Sprintf("room_name:%s", roomName), roomKey, 0)

	// Sort the room
	rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: fmt.Sprintf("room:%d", roomID), Score: 1})

	c.JSON(http.StatusCreated, gin.H{"message": "Room was created successfully", "roomId": roomID})
}

func joinRoom(c *gin.Context) {

	var requestData struct {
		RoomID     int    `json:"roomID"`
		ClientID   int    `json:"clientID"`
		RpcAddress string `json:"rpcAddrerss"`
		ClientName string `json:"clientName"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	}

	var roomID int = requestData.RoomID
	var clientID int = requestData.ClientID
	var rpcAddress string = requestData.RpcAddress
	var clientName string = requestData.ClientName

	roomKey := fmt.Sprintf("room:%d", roomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Room does not exist")})
		return
	}

	// Check if client is already in the room
	clientInRoom := rdb.SIsMember(context.Background(), roomKey+":clients", clientID).Val()
	if clientInRoom {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Client already in the room")})
		return
	}

	// Add clientID to the room's set of clients
	err := rdb.SAdd(context.Background(), roomKey+":clients", clientID).Err()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error adding client to room: %v", err)})
		return
	}

	currentPlayers, err := rdb.HIncrBy(context.Background(), roomKey, "current_players", 1).Result()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error joining room: %v", err)})
		return
	}

	// Update the sorted set with the new player count
	err = rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: roomKey, Score: float64(currentPlayers)}).Err()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error updating room player count in sorted set: %v", err)})
		return
	}

	err = rdb.SAdd(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddress+";"+clientName).Err()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error adding client name to room: %v", err)})
		return
	}

	maxPlayers, err := rdb.HGet(context.Background(), roomKey, "max_players").Int64()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error reading room details: %v", err)})
		return
	}

	if currentPlayers > maxPlayers {
		rdb.HIncrBy(context.Background(), roomKey, "current_players", -1)
		rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: roomKey, Score: float64(currentPlayers - 1)})
		rdb.SRem(context.Background(), roomKey+":clients", clientID)
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Room is full")})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Joined room successfully!", "roomId": roomID})

}

func leaveRoom(c *gin.Context) {

	var requestData struct {
		RoomID     int    `json:"roomID"`
		ClientID   int    `json:"clientID"`
		RpcAddress string `json:"rpcAddrerss"`
		ClientName string `json:"clientName"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	}

	var roomID int = requestData.RoomID

	roomKey := fmt.Sprintf("room:%d", roomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Room does not exist")})
		return
	}

	// Decrement the player count
	_, err := rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Result()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error leaving room: %v", err)})
		return
	}

	// Update current player count in the sorted set
	rdb.ZIncrBy(context.Background(), "rooms", -1, fmt.Sprintf("room:%d", roomID))

	c.JSON(http.StatusCreated, gin.H{"message": "Joined room successfully!", "roomId": roomID})

}

func listRooms(c *gin.Context) {

	roomKeys := rdb.Keys(context.Background(), "room:*").Val()
	var rooms []map[string]string

	for _, roomKey := range roomKeys {
		// Check the type of the key to ensure it is a hash
		keyType := rdb.Type(context.Background(), roomKey).Val()
		if keyType != "hash" {
			log.Printf("Skipping key %s of type %s", roomKey, keyType)
			continue
		}

		room, err := rdb.HGetAll(context.Background(), roomKey).Result()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Errorf("Error reading room details: %v", err)})
			return
		}
		rooms = append(rooms, room)
	}

	c.JSON(http.StatusOK, gin.H{"rooms": rooms})

}

func roomDetails(c *gin.Context) {

	var requestData struct {
		RoomID     int    `json:"roomID"`
		ClientID   int    `json:"clientID"`
		RpcAddress string `json:"rpcAddrerss"`
		ClientName string `json:"clientName"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return // Added return statement to terminate the function on error
	}

	var roomID int = requestData.RoomID

	roomKey := fmt.Sprintf("room:%d", roomID)
	roomData, err := rdb.HGetAll(context.Background(), roomKey).Result()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rpcName, err := rdb.SMembers(context.Background(), fmt.Sprintf("room:%d:rpc_addresses", roomID)).Result()
	if err != nil {
		log.Printf("Error fetching client names: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch client names"}) // Return an error if needed
		return
	}

	// Create a response structure to hold both roomData and rpcName
	response := struct {
		RoomData map[string]string `json:"roomData"`
		RpcNames []string          `json:"rpcNames"`
	}{
		RoomData: roomData,
		RpcNames: rpcName,
	}

	// Return the response as JSON
	c.JSON(http.StatusOK, response)
}

func transferHost(c *gin.Context) {

	var requestData struct {
		RoomID            int    `json:"roomID"`
		CurrentRPCAddress string `json:"currentRpcAddress"`
		NewHostRPCAddress string `json:"newHostRpcAddress"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return // Added return statement to terminate the function on error
	}

	var roomID int = requestData.RoomID
	var currentRpcAddress = requestData.CurrentRPCAddress
	var newHostRpcAddress = requestData.NewHostRPCAddress

	roomKey := fmt.Sprintf("room:%d", roomID)

	// Get the current host rpc address
	currentHostRpcAddress, err := rdb.HGet(context.Background(), roomKey, "host_rpc_address").Result()
	if err != nil {
		c.JSON(http.StatusBadRequest, fmt.Errorf("Error fetching current host: %v", err))
		return
	}

	// Verify if the current user (based on rpcAddress) is the host
	if currentHostRpcAddress != currentRpcAddress {
		c.JSON(http.StatusBadRequest, fmt.Errorf("You are not the host of this room"))
		return
	}

	// Verify if the current new host is not the current one
	if currentRpcAddress == newHostRpcAddress {
		c.JSON(http.StatusBadRequest, fmt.Errorf("You can not transfer the host to yourself"))
		return
	}

	// Check if the new host rpc address is in the room
	trimmedNewHostRpcAddress := strings.TrimSpace(newHostRpcAddress)
	isMember := rdb.SIsMember(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), trimmedNewHostRpcAddress).Val()
	if !isMember {
		c.JSON(http.StatusBadRequest, fmt.Errorf("The provided new host rpc address is not a member of the room"))
		return
	}

	// Set the new host rpc address
	err = rdb.HSet(context.Background(), roomKey, "host_rpc_address", trimmedNewHostRpcAddress).Err()
	if err != nil {
		c.JSON(http.StatusBadRequest, fmt.Errorf("Error transferring host role: %v", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Host transfer was successful", "oldhost": currentHostRpcAddress, "newhost": newHostRpcAddress, "roomid": roomID, "roomkey": roomKey})

}
