package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"pulsecore/proto/proto"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	exitCmd           = "exit"
	heartbeatInterval = 1 * time.Second
)

type Client struct {
	proto.UnimplementedGameServiceServer
	MyAddress string
}

var CurrentRoomID int = -1

var ClientName string

var serverAddr string

var applicationId string

var MyRPCAddress string

func main() {

	var redisAddr string

	u := uuid.New()
	shortUUID := u.String()[:8]
	defaultName := fmt.Sprintf("Player_%s", shortUUID)

	// Parsing provided terminal arguments
	// Specify a proper address of the server
	// If the server is running on Docker network, locally on your machine
	// then the ip address of the Docker host is usually 127.0.0.1
	// You can speicify it as  127.0.0.1:12345 and run the client locally.
	// In case the server is deployed in a remote cloud, then use a proper external ip and the port
	// of the service.
	flag.StringVar(&serverAddr, "server", "pulsecore_server_0:12345", "Specify server address you want to connect with")
	flag.StringVar(&applicationId, "app", "", "Specify your registered applicaiton")

	// For redis-server you would use 127.0.0.1:6379 if it's running on Docker locally
	// If it's deployed in a remote cloud system, then get it's appropriate external id and the port
	flag.StringVar(&redisAddr, "redis-server", "", "Specify your redis address.\nOn local machine it is usually localhost:6379")
	flag.StringVar(&ClientName, "name", defaultName, "A name for the client")
	flag.Parse()

	if applicationId == "" {
		log.Fatal("Application id can not be empty")
	}

	// Check if applicationId exists in the database
	appExists := checkAppIdExists(applicationId)
	if !appExists {
		log.Fatal("No such app identifier registered")
	}

	// Check if the provided server is associated with the applicationId
	serverExists := checkServerAssociated(applicationId, serverAddr)
	if !serverExists {
		log.Fatal("There's no such server registered with this application id")
	}

	if redisAddr == "" {
		log.Fatal("Redis server address required for room management within the server!\n",
			"On local machines it is usually localhost:6379")
	}

	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Client name:", ClientName)
	fmt.Println(strings.Repeat("=", 50))

	// Set up a connection to the server
	conn, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Did not connect: %v", err)
	}
	defer conn.Close()

	client := proto.NewGameServiceClient(conn)

	// Start listening on a dynamically allocated port
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("Failed to listen on a port: %v", err)
	}
	defer lis.Close()
	dynamicPort := lis.Addr().(*net.TCPAddr).Port
	fmt.Printf("Listening on dynamically allocated port: %d\n", dynamicPort)
	fmt.Println(strings.Repeat("=", 50))

	// Register the client with the dynamically allocated port
	var host string
	host = "host.docker.internal"

	rpcAddress := fmt.Sprintf("%s:%d", host, dynamicPort)

	regResp, err := registerClient(client, rpcAddress)
	if err != nil || !regResp.GetSuccess() {
		log.Fatalf("Failed to register client: %v", err)
	}
	fmt.Println("Client registered successfully!")
	fmt.Println(strings.Repeat("=", 50))

	done := make(chan bool)

	// Start a go routine to send heartbeats regularly and handle challenges
	go func() {
		for {
			time.Sleep(heartbeatInterval) // Wait for heartbeatInterval duration

			resp, err := sendHeartbeat(client, rpcAddress)
			if err != nil || !resp.GetSuccess() {
				log.Printf("Failed to send heartbeat or server rejected heartbeat: %v", err)
			}

			if resp.GetChallengeQuestion() != "" {
				if !solveChallenge(resp.GetChallengeQuestion()) {
					log.Println("Failed to solve server's challenge. Stopping heartbeats.")
					done <- true
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-done:
				log.Println("Terminating the main program due to failed challenge.")
				os.Exit(1)
			default:
				time.Sleep(time.Millisecond * 100)
			}
		}
	}()

	clientServer := grpc.NewServer()
	proto.RegisterGameServiceServer(clientServer, &Client{MyAddress: rpcAddress})
	go func() {
		if err := clientServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	var rdb *redis.Client = &redis.Client{}
	defer rdb.Close()

	// Redis setup
	rdb = redis.NewClient(&redis.Options{
		Addr:     redisAddr, // E.g. redis01:6379
		Password: "",
		DB:       0,
	})

	if rdb.Get(context.Background(), "room_id").Err() == redis.Nil {
		rdb.Set(context.Background(), "room_id", 0, 0)
	}

	_, err = rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Unable to connect to Redis: %v", err)
	}
	fmt.Println("Connected to redis...")
	fmt.Println(strings.Repeat("=", 50))

	// Send messages in an infinite loop until the user types "exit"
	reader := bufio.NewReader(os.Stdin)

	MyRPCAddress = rpcAddress + ";" + ClientName
	choiceHandler(reader, client, rdb, dynamicPort, rpcAddress)

}

func solveChallenge(challengeQuestion string) bool {
	if challengeQuestion == "What is 2 + 2?" {
		return true
	}
	return false
}

func checkAppIdExists(appId string) bool {
	requestData := struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}{
		TableName:  "applications",
		Conditions: map[string]interface{}{"app_identifier": appId},
	}

	resp, err := sendPostRequest("http://localhost:8091/database/records/query", requestData)
	if err != nil {
		log.Fatal("Failed to make a request to the database: ", err)
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(resp, &results); err != nil {
		log.Fatal("Failed to decode the response: ", err)
	}

	return len(results) > 0
}

func checkServerAssociated(appId, server string) bool {
	requestData := struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}{
		TableName:  "server_addresses",
		Conditions: map[string]interface{}{"app_identifier": appId},
	}

	resp, err := sendPostRequest("http://localhost:8091/database/records/query", requestData)
	if err != nil {
		log.Fatal("Failed to make a request to the database: ", err)
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(resp, &results); err != nil {
		log.Fatal("Failed to decode the response: ", err)
	}

	for _, result := range results {
		if result["address"] == server {
			return true
		}
	}
	return false
}

func sendPostRequest(url string, data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func choiceHandler(reader *bufio.Reader, client proto.GameServiceClient, rdb *redis.Client, dynamicPort int, rpcAddress string) {
	for {
		printSeparator := func() {
			fmt.Println(strings.Repeat("=", 50))
		}
		printSeparator()
		fmt.Println("1: Send a message")
		fmt.Println("2: Create a room")
		fmt.Println("3: Join a room")
		fmt.Println("4: Auto-join available room")
		fmt.Println("5: Leave a room")
		fmt.Println("6: List rooms")
		fmt.Println("7: View room details")
		fmt.Println("8: Current Room")
		fmt.Println("9: Room-specific message")
		fmt.Println("10: Change the host")
		fmt.Println("11: My RPC Address")
		fmt.Println("12: Exit")
		printSeparator()
		fmt.Print("Enter your choice: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			printSeparator()
			fmt.Print("Enter your message: ")
			message, _ := reader.ReadString('\n')
			message = strings.TrimSpace(message)
			msgResp, err := sendMessageToServer(client, message)
			if err != nil || !msgResp.GetSuccess() {
				log.Printf("Failed to send message to server: %v", err)
			} else {
				fmt.Println("Message sent successfully!")
			}
		case "2":
			printSeparator()
			if CurrentRoomID != -1 {
				fmt.Println("You are already connected to a room. Please leave the current room before creating a new one.")
				continue
			}
			fmt.Print("Enter room name: ")
			roomName, _ := reader.ReadString('\n')
			roomName = strings.TrimSpace(roomName)

			roomID, err := createRoom(rdb, roomName, rpcAddress)
			if err != nil {
				log.Printf("Error creating room: %v", err)
			} else {
				fmt.Println("Room created successfully with ID:", roomID)
				CurrentRoomID = roomID
			}
		case "3":
			printSeparator()
			if CurrentRoomID != -1 {
				fmt.Println("You are already connected to a room. Please leave the current room before joining another.")
				continue
			}
			fmt.Print("Enter room ID to join: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, err := strconv.Atoi(strings.TrimSpace(roomIDStr))
			if err != nil {
				log.Printf("Invalid room ID: %v", err)
				continue
			}

			err = joinRoom(rdb, roomID, dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error joining room: %v", err)
			} else {
				CurrentRoomID = roomID
			}
		case "4":
			printSeparator()
			if CurrentRoomID != -1 {
				fmt.Println("You are already connected to a room. Please leave the current room before joining another.")
				continue
			}

			roomID, err := joinAvailableRoom(rdb, dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error auto-joining room: %v", err)
			} else {
				CurrentRoomID = roomID
			}
		case "5":
			printSeparator()
			err := leaveCurrentRoom(rdb, dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error leaving room: %v", err)
			} else {
				fmt.Println("Left room successfully!")
			}
		case "6":
			printSeparator()
			rooms, err := listRooms(rdb)
			if err != nil {
				log.Printf("Error listing rooms: %v", err)
			} else {
				fmt.Println("Available rooms:")
				for _, room := range rooms {
					if room["room_id"] == "" {
						log.Println("Encountered room with empty room_id")
						continue
					}
					roomID, err := strconv.Atoi(room["room_id"])
					if err != nil {
						log.Printf("Error converting room ID: %s\n", err)
						continue
					}
					fmt.Printf("ID: %d, Name: %s, Current Players: %s/%s, Status: %s\n",
						roomID, room["room_name"], room["current_players"],
						room["max_players"], room["status"])
				}
			}
		case "7":
			printSeparator()
			fmt.Print("Enter room ID to view details: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			err := printRoomData(rdb, roomID)
			if err != nil {
				log.Printf("Error fetching room data: %v", err)
			}
		case "8":
			printSeparator()
			err := printRoomData(rdb, CurrentRoomID)
			if err != nil {
				log.Printf("Error fetching current room data: %v", err)
			}
		case "9":
			printSeparator()
			if CurrentRoomID == -1 {
				fmt.Println("You are not connected to any room. Please join a room first before sending a room-specific message.")
				continue
			}
			printSeparator()
			fmt.Print("Enter your room-specific message: ")
			message, _ := reader.ReadString('\n')
			message = strings.TrimSpace(message)
			msgResp, err := sendRoomMessageToServer(client, CurrentRoomID, message)
			if err != nil {
				log.Printf("Failed to send room message to server due to an error: %v", err)
			} else if !msgResp.GetSuccess() {
				log.Printf("Failed to send room message to server: Message not successful.")
			} else {
				fmt.Println("Room message sent successfully!")
			}
		case "10":
			printSeparator()
			fmt.Println("Enter a new host:")
			newHostRPCAddress, _ := reader.ReadString('\n')
			err := transferHost(rdb, CurrentRoomID, MyRPCAddress, newHostRPCAddress)
			if err != nil {
				fmt.Println("Error happed during the host transfer...")
				fmt.Printf("Error meesage: %s\n", err.Error())
				continue
			}
		case "11":
			printSeparator()
			fmt.Println(MyRPCAddress)

		case "12":
			printSeparator()
			fmt.Println("Exiting...")
			return
		default:
			printSeparator()
			fmt.Println("Invalid choice. Please try again.")
		}
	}
}

func sendRoomMessageToServer(client proto.GameServiceClient, roomID int, message string) (*proto.MessageResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return client.SendMessageToRoom(ctx, &proto.MessageToRoomRequest{
		RoomID:  strconv.Itoa(roomID),
		Message: message,
	})
}

func registerClient(c proto.GameServiceClient, rpcAddress string) (*proto.RegisterClientResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	return c.RegisterClient(ctx, &proto.RegisterClientRequest{RpcAddress: rpcAddress})
}

func sendHeartbeat(c proto.GameServiceClient, clientId string) (*proto.HeartbeatResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return c.SendHeartbeat(ctx, &proto.HeartbeatRequest{ClientId: clientId})
}

func sendMessageToServer(c proto.GameServiceClient, message string) (*proto.SendMessageResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return c.SendMessageToServer(ctx, &proto.SendMessageRequest{Message: message})
}

func broadcastMessage(c proto.GameServiceClient, message string) (*proto.MessageResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return c.BroadcastMessage(ctx, &proto.MessageRequest{Message: message})
}

func (c *Client) ReceiveMessageFromServer(ctx context.Context, req *proto.MessageFromServerRequest) (*proto.MessageFromServerResponse, error) {
	if req.SenderAddress != c.MyAddress && req.SenderAddress != serverAddr {
		fmt.Println("\n")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Printf("\nReceived message from another client: %s\n", req.Message)
	}
	fmt.Println("\n")
	fmt.Println(strings.Repeat("=", 50))
	return &proto.MessageFromServerResponse{Success: true}, nil
}

func (c *Client) SolveChallenge(ctx context.Context, req *proto.ChallengeRequest) (*proto.ChallengeResponse, error) {
	answer := "4"
	return &proto.ChallengeResponse{Answer: answer}, nil
}

func createRoom(rdb *redis.Client, roomName string, rpcAddress string) (int, error) {

	// Check if a room with the given name already exists
	existingRoomKey := rdb.Keys(context.Background(), fmt.Sprintf("room_name:%s", roomName)).Val()
	if len(existingRoomKey) > 0 {
		return -1, fmt.Errorf("A room with name %s already exists", roomName)
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
		"host_rpc_address", rpcAddress+";"+ClientName,
		"date_created", time.Now().Format(time.RFC3339),
	).Err()

	if err != nil {
		currentValue := rdb.Get(context.Background(), "room_id").Val()
		if currentValue != "0" {
			rdb.Decr(context.Background(), "room_id")
		}
		return -1, fmt.Errorf("Error creating room: %v", err)
	}

	// Add the client name to the room's set of clients' names
	err = rdb.SAdd(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddress+";"+ClientName).Err()
	if err != nil {
		return -1, fmt.Errorf("Error adding client name to room: %v", err)
	}

	// Associate the room name with its key in Redis
	rdb.Set(context.Background(), fmt.Sprintf("room_name:%s", roomName), roomKey, 0)

	// Sort the room
	rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: fmt.Sprintf("room:%d", roomID), Score: 1})

	return int(roomID), nil
}

// Transfer the host role to another member
func transferHost(rdb *redis.Client, roomID int, currentRpcAddress string, newHostRpcAddress string) error {

	roomKey := fmt.Sprintf("room:%d", roomID)

	// Get the current host rpc address
	currentHostRpcAddress, err := rdb.HGet(context.Background(), roomKey, "host_rpc_address").Result()
	if err != nil {
		return fmt.Errorf("Error fetching current host: %v", err)
	}

	// Verify if the current user (based on rpcAddress) is the host
	if currentHostRpcAddress != currentRpcAddress {
		return fmt.Errorf("You are not the host of this room")
	}

	// Verify if the current new host is not the current one
	if currentRpcAddress == newHostRpcAddress {
		return fmt.Errorf("You can not transfer the host to yourself")
	}

	// Check if the new host rpc address is in the room
	trimmedNewHostRpcAddress := strings.TrimSpace(newHostRpcAddress)
	isMember := rdb.SIsMember(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), trimmedNewHostRpcAddress).Val()
	if !isMember {
		return fmt.Errorf("The provided new host rpc address is not a member of the room")
	}

	// Set the new host rpc address
	err = rdb.HSet(context.Background(), roomKey, "host_rpc_address", trimmedNewHostRpcAddress).Err()
	if err != nil {
		return fmt.Errorf("Error transferring host role: %v", err)
	}

	return nil
}

func joinAvailableRoom(rdb *redis.Client, dynamicPort int, rpcAddress string) (int, error) {
	// Get the room with the lowest current player count from the sorted set
	// Runtime complexity is O(log(N))
	roomKey := rdb.ZRangeWithScores(context.Background(), "rooms", 0, 0).Val()

	// Extract roomID and current player count
	roomIDStr := strings.Split(roomKey[0].Member.(string), ":")[1]
	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		return -1, fmt.Errorf("Error parsing room ID: %v", err)
	}
	currentPlayers := int(roomKey[0].Score)

	// Retrieve max players as a string from Redis
	maxPlayersStr := rdb.HGet(context.Background(), fmt.Sprintf("room:%d", roomID), "max_players").Val()

	// Convert the maxPlayers string to an integer
	maxPlayers, err := strconv.Atoi(maxPlayersStr)
	if err != nil {
		return -1, fmt.Errorf("Error converting max players string to int: %v", err)
	}

	// Check if room is available
	if currentPlayers >= maxPlayers {
		return -1, fmt.Errorf("No available rooms")
	}

	// Now, attempt to join this room
	err = joinRoom(rdb, roomID, dynamicPort, rpcAddress)
	if err != nil {
		return -1, fmt.Errorf("Error joining room: %v", err)
	}

	// Update current player count in the sorted set
	rdb.ZIncrBy(context.Background(), "rooms", 1, fmt.Sprintf("room:%d", roomID))

	CurrentRoomID = roomID
	return roomID, nil
}

func joinRoom(rdb *redis.Client, roomID int, clientID int, rpcAddress string) error {
	roomKey := fmt.Sprintf("room:%d", roomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		return fmt.Errorf("Room does not exist")
	}

	// Check if client is already in the room
	clientInRoom := rdb.SIsMember(context.Background(), roomKey+":clients", clientID).Val()
	if clientInRoom {
		return fmt.Errorf("Client already in the room")
	}

	// Add clientID to the room's set of clients
	err := rdb.SAdd(context.Background(), roomKey+":clients", clientID).Err()
	if err != nil {
		return fmt.Errorf("Error adding client to room: %v", err)
	}

	currentPlayers, err := rdb.HIncrBy(context.Background(), roomKey, "current_players", 1).Result()
	if err != nil {
		return fmt.Errorf("Error joining room: %v", err)
	}

	// Update the sorted set with the new player count
	err = rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: roomKey, Score: float64(currentPlayers)}).Err()
	if err != nil {
		return fmt.Errorf("Error updating room player count in sorted set: %v", err)
	}

	err = rdb.SAdd(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddress+";"+ClientName).Err()
	if err != nil {
		return fmt.Errorf("Error adding client name to room: %v", err)
	}

	maxPlayers, err := rdb.HGet(context.Background(), roomKey, "max_players").Int64()
	if err != nil {
		return fmt.Errorf("Error reading room details: %v", err)
	}

	if currentPlayers > maxPlayers {
		rdb.HIncrBy(context.Background(), roomKey, "current_players", -1)
		rdb.ZAdd(context.Background(), "rooms", &redis.Z{Member: roomKey, Score: float64(currentPlayers - 1)})
		rdb.SRem(context.Background(), roomKey+":clients", clientID)
		return fmt.Errorf("Room is full")
	}

	fmt.Println("Joined room successfully!")
	return nil
}

func leaveRoom(rdb *redis.Client, roomID int) error {
	roomKey := fmt.Sprintf("room:%d", roomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		return fmt.Errorf("Room does not exist")
	}

	// Decrement the player count
	_, err := rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Result()
	if err != nil {
		return fmt.Errorf("Error leaving room: %v", err)
	}

	// Update current player count in the sorted set
	rdb.ZIncrBy(context.Background(), "rooms", -1, fmt.Sprintf("room:%d", roomID))

	return nil
}

func leaveCurrentRoom(rdb *redis.Client, clientID int, rpcAddress string) error {
	if CurrentRoomID == -1 {
		return fmt.Errorf("Client is not in any room")
	}

	roomKey := fmt.Sprintf("room:%d", CurrentRoomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		CurrentRoomID = -1 // Ensure to reset the room ID to -1
		return fmt.Errorf("Room does not exist")
	}

	// Remove clientID from the room's set of clients
	err := rdb.SRem(context.Background(), roomKey+":clients", clientID).Err()
	if err != nil {
		return fmt.Errorf("Error removing client from room: %v", err)
	}

	// Decrement the player count
	_, err = rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Result()
	if err != nil {
		return fmt.Errorf("Error decrementing player count: %v", err)
	}

	rdb.SRem(context.Background(), fmt.Sprintf("room:%d:rpc_addresses", CurrentRoomID), rpcAddress+";"+ClientName)

	// Reset CurrentRoomID
	CurrentRoomID = -1
	return nil
}

func listRooms(rdb *redis.Client) ([]map[string]string, error) {
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
			return nil, fmt.Errorf("Error reading room details: %v", err)
		}
		rooms = append(rooms, room)
	}

	return rooms, nil
}

func printRoomData(rdb *redis.Client, roomID int) error {
	roomKey := fmt.Sprintf("room:%d", roomID)
	roomData, err := rdb.HGetAll(context.Background(), roomKey).Result()
	if err != nil {
		return fmt.Errorf("Error fetching room data: %v", err)
	}

	for key, value := range roomData {
		fmt.Printf("%s: %s\n", key, value)
	}

	rpcName, err := rdb.SMembers(context.Background(), fmt.Sprintf("room:%d:rpc_addresses", roomID)).Result()
	if err != nil {
		log.Printf("Error fetching client names: %v", err)
	} else {
		fmt.Printf("Players in room: %v\n", strings.Join(rpcName, ", "))
	}

	fmt.Println(strings.Repeat("=", 50))

	return nil
}

// ---------------ROOM MANAGEMENT-----------------------------

func (gc *Client) BroadcastMessage(ctx context.Context, req *proto.MessageRequest) (*proto.MessageResponse, error) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Received message:", req.GetMessage())
	return &proto.MessageResponse{Reply: "Received the message"}, nil
}
