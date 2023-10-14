package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"pulsecore/proto/proto"

	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc"
)

const (
	serverAddr        = "localhost:12345"
	exitCmd           = "exit"
	heartbeatInterval = 10 * time.Second
)

type Client struct {
	proto.UnimplementedGameServiceServer
	MyAddress string
}

var CurrentRoomID int = -1

func main() {
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

	// Register the client with the dynamically allocated port
	rpcAddress := fmt.Sprintf("localhost:%d", dynamicPort)
	regResp, err := registerClient(client, rpcAddress)
	if err != nil || !regResp.GetSuccess() {
		log.Fatalf("Failed to register client: %v", err)
	}
	fmt.Println("Client registered successfully!")

	// Start a go routine to send heartbeats regularly
	go func() {
		for {
			time.Sleep(heartbeatInterval) // Wait for heartbeatInterval duration
			_, err := sendHeartbeat(client, rpcAddress)
			if err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
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
		Addr:     "localhost:6379",
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

	// Send messages in an infinite loop until the user types "exit"
	reader := bufio.NewReader(os.Stdin)
	choiceHandler(reader, client, rdb, dynamicPort)

}

func choiceHandler(reader *bufio.Reader, client proto.GameServiceClient, rdb *redis.Client, dynamicPort int) {
	for {
		printSeparator := func() {
			fmt.Println(strings.Repeat("=", 50))
		}
		fmt.Println("1: Send a message")
		fmt.Println("2: Create a room")
		fmt.Println("3: Join a room")
		fmt.Println("4: Leave a room")
		fmt.Println("5: List rooms")
		fmt.Println("6: View room details")
		fmt.Println("7: Current Room")
		fmt.Println("8: Exit")
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

			roomID, err := createRoom(rdb, roomName, dynamicPort)
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

			err = joinRoom(rdb, roomID, dynamicPort)
			if err != nil {
				log.Printf("Error joining room: %v", err)
			} else {
				fmt.Println("Joined room successfully!")
				CurrentRoomID = roomID
			}
		case "4":
			printSeparator()
			err := leaveCurrentRoom(rdb)
			if err != nil {
				log.Printf("Error leaving room: %v", err)
			} else {
				fmt.Println("Left room successfully!")
			}
		case "5":
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
		case "6":
			printSeparator()
			fmt.Print("Enter room ID to view details: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			err := printRoomData(rdb, roomID)
			if err != nil {
				log.Printf("Error fetching room data: %v", err)
			}
		case "7":
			printSeparator()
			err := printRoomData(rdb, CurrentRoomID)
			if err != nil {
				log.Printf("Error fetching current room data: %v", err)
			}
		case "8":
			printSeparator()
			fmt.Println("Exiting...")
			return
		default:
			fmt.Println("Invalid choice. Please try again.")
		}
	}
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
		fmt.Printf("Received message from another client: %s\n", req.Message)
	}
	return &proto.MessageFromServerResponse{Success: true}, nil
}

func createRoom(rdb *redis.Client, roomName string, hostId int) (int, error) {

	// Check if a room with the given name already exists
	existingRoomKey := rdb.Keys(context.Background(), fmt.Sprintf("room_name:%s", roomName)).Val()
	if len(existingRoomKey) > 0 {
		return -1, fmt.Errorf("A room with name %s already exists", roomName)
	}

	// Increment the room_id to get a new room ID
	roomID := rdb.Incr(context.Background(), "room_id").Val()

	roomKey := fmt.Sprintf("room:%d", roomID)
	err := rdb.HMSet(context.Background(), roomKey,
		"room_id", roomID, // Add this line
		"room_name", roomName,
		"host_id", hostId,
		"max_players", 10,
		"current_players", 1,
		"server_id", serverAddr,
		"status", "available",
		"properties", "{}",
		"date_created", time.Now().Format(time.RFC3339),
	).Err()

	if err != nil {
		currentValue := rdb.Get(context.Background(), "room_id").Val()
		if currentValue != "0" {
			rdb.Decr(context.Background(), "room_id")
		}
		return -1, fmt.Errorf("Error creating room: %v", err)
	}

	// Associate the room name with its key in Redis
	rdb.Set(context.Background(), fmt.Sprintf("room_name:%s", roomName), roomKey, 0)

	return int(roomID), nil
}

func joinRoom(rdb *redis.Client, roomID int, clientID int) error {
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

	maxPlayers, err := rdb.HGet(context.Background(), roomKey, "max_players").Int64()
	if err != nil {
		return fmt.Errorf("Error reading room details: %v", err)
	}

	if currentPlayers > maxPlayers {
		// Decrement back as we've already incremented it
		rdb.HIncrBy(context.Background(), roomKey, "current_players", -1)
		// Remove the client from the set as the room is full
		rdb.SRem(context.Background(), roomKey+":clients", clientID)
		return fmt.Errorf("Room is full")
	}

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

	return nil
}

func leaveCurrentRoom(rdb *redis.Client) error {
	roomKey := fmt.Sprintf("room:%d", CurrentRoomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		return fmt.Errorf("Room does not exist")
	}

	// Remove clientID from the room's set of clients
	err := rdb.SRem(context.Background(), roomKey+":clients", CurrentRoomID).Err()
	if err != nil {
		return fmt.Errorf("Error removing client from room: %v", err)
	}

	// Decrement the player count
	_, err = rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Result()
	if err != nil {
		return fmt.Errorf("Error decrementing player count: %v", err)
	}

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

	fmt.Println(strings.Repeat("=", 50))

	return nil
}

// ---------------ROOM MANAGEMENT-----------------------------
