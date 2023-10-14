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

var CurrentRoomID int

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
	go func() {
		rdb = redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		})

		_, err := rdb.Ping(context.Background()).Result()
		if err != nil {
			log.Fatalf("Unable to connect to Redis: %v", err)
		}
		fmt.Println("Connected to redis...")
	}()

	// Send messages in an infinite loop until the user types "exit"
	reader := bufio.NewReader(os.Stdin)
	for {
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
			fmt.Print("Enter room name: ")
			roomName, _ := reader.ReadString('\n')
			roomName = strings.TrimSpace(roomName)

			roomID, err := createRoom(rdb, roomName, dynamicPort)
			if err != nil {
				log.Printf("Error creating room: %v", err)
			} else {
				fmt.Println("Room created successfully with ID:", roomID)
				CurrentRoomID = roomID // Store the room ID in the client struct
			}

		case "3":
			fmt.Print("Enter room ID to join: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			if roomID == CurrentRoomID {
				fmt.Println("You are already in this room and cannot join again.")
				continue
			}

			err := joinRoom(rdb, roomID)
			if err != nil {
				log.Printf("Error joining room: %v", err)
			} else {
				fmt.Println("Joined room successfully!")
				CurrentRoomID = roomID // Update the room ID in the client struct
			}

		case "4":
			fmt.Print("Enter room ID to leave: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			err := leaveRoom(rdb, roomID)
			if err != nil {
				log.Printf("Error leaving room: %v", err)
			} else {
				fmt.Println("Left room successfully!")
			}

		case "5":
			rooms, err := listRooms(rdb)
			if err != nil {
				log.Printf("Error listing rooms: %v", err)
			} else {
				fmt.Println("Available rooms:")
				for _, room := range rooms {
					fmt.Printf("ID: %s, Name: %s, Current Players: %s/%s, Status: %s\n",
						room["room_id"], room["room_name"], room["current_players"],
						room["max_players"], room["status"])
				}
			}

		case "6":
			fmt.Print("Enter room ID to view details: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			err := printRoomData(rdb, roomID)
			if err != nil {
				log.Printf("Error fetching room data: %v", err)
			}

		case "7":
			err := printRoomData(rdb, CurrentRoomID)
			if err != nil {
				log.Printf("Error fetching current room data: %v", err)
			}

		case "8":
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
	// Handle the incoming message here
	if req.SenderAddress != c.MyAddress && req.SenderAddress != serverAddr {
		fmt.Printf("Received message from another client: %s\n", req.Message)
	}
	return &proto.MessageFromServerResponse{Success: true}, nil
}

// ---------------ROOM MANAGEMENT-----------------------------

func createRoom(rdb *redis.Client, roomName string, hostId int) (int, error) {

	existingRoomKey := rdb.Keys(context.Background(), fmt.Sprintf("room_name:%s", roomName)).Val()
	if len(existingRoomKey) > 0 {
		return -1, fmt.Errorf("A room with name %s already exists", roomName)
	}

	roomID := rdb.Incr(context.Background(), "room_id").Val()

	roomKey := fmt.Sprintf("room:%d", roomID)
	err := rdb.HMSet(context.Background(), roomKey,
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
		return -1, fmt.Errorf("Error creating room: %v", err)
	}

	rdb.Set(context.Background(), fmt.Sprintf("room_name:%s", roomName), roomKey, 0)

	return int(roomID), nil
}

func joinRoom(rdb *redis.Client, roomID int) error {
	roomKey := fmt.Sprintf("room:%d", roomID)
	exists := rdb.Exists(context.Background(), roomKey).Val()

	if exists == 0 {
		return fmt.Errorf("Room does not exist")
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

func listRooms(rdb *redis.Client) ([]map[string]string, error) {
	var rooms []map[string]string

	iter := rdb.Scan(context.Background(), 0, "room:*", 0).Iterator()
	for iter.Next(context.Background()) {
		roomKey := iter.Val()
		room, err := rdb.HGetAll(context.Background(), roomKey).Result()

		if err != nil {
			return nil, fmt.Errorf("Error reading room details: %v", err)
		}

		rooms = append(rooms, room)
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("Error listing rooms: %v", err)
	}

	return rooms, nil
}

func printRoomData(rdb *redis.Client, roomID int) error {
	roomKey := fmt.Sprintf("room:%d", roomID)
	roomData, err := rdb.HGetAll(context.Background(), roomKey).Result()
	if err != nil {
		return fmt.Errorf("Error fetching room data: %v", err)
	}

	fmt.Println("Room Data:")
	for key, value := range roomData {
		fmt.Printf("%s: %s\n", key, value)
	}

	return nil
}

// ---------------ROOM MANAGEMENT-----------------------------
