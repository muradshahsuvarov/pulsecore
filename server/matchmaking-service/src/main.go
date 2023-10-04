package main

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// Health check endpoint
	r.GET("/matchmaking/health", checkHealth)

	// Get a list of available rooms
	r.GET("/matchmaking/rooms", listAvailableRooms)

	// Create a new game room
	r.POST("/matchmaking/create-room", createRoom)

	// Join a specific room
	r.POST("/matchmaking/join-room/:roomID", joinRoom)

	// Leave a specific room
	r.DELETE("/matchmaking/leave-room/:roomID", leaveRoom)

	// Connect to the master server
	r.POST("/matchmaking/connect", connectToMaster)

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
	// List available game rooms for players to join
	// This data might come from a database or in-memory storage

	rooms := []string{"Room1", "Room2"} // Sample data
	c.JSON(200, rooms)
}

func createRoom(c *gin.Context) {
	// Logic to create a new game room and return its details

	roomID := "Room1234" // This would typically be generated dynamically
	c.JSON(201, gin.H{
		"roomID": roomID,
	})
}

func joinRoom(c *gin.Context) {
	// Logic for a player to join a specific game room

	roomID := c.Param("roomID")
	// Further logic to join the room, for instance, checking if room has space, etc.
	c.JSON(200, gin.H{
		"message": "Joined room successfully",
		"roomID":  roomID,
	})
}

func leaveRoom(c *gin.Context) {
	// Logic for a player to leave a specific game room

	roomID := c.Param("roomID")
	// Further logic to leave the room, like updating room's players count, etc.
	c.JSON(200, gin.H{
		"message": "Left room successfully",
		"roomID":  roomID,
	})
}

func connectToMaster(c *gin.Context) {
	// Here, we might handle things like:
	// - Player authentication (if required)
	// - Fetch player's current game state or stats
	// - Generate a session token or ID for further secured interactions

	// For simplicity, let's assume we return a static session token:
	sessionToken := "1234567890abcdef"

	c.JSON(200, gin.H{
		"message":      "Connected to master server successfully",
		"sessionToken": sessionToken,
	})
}
