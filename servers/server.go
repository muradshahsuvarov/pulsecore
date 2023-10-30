package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"pulsecore/proto/proto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

var currentPlayers int
var playerCountMutex = sync.Mutex{}

const (
	heartbeatInterval       = 1 * time.Second
	missedHeartbeatsAllowed = 3
)

type ClientInfo struct {
	LastHeartbeat time.Time
	RPCClient     proto.GameServiceClient
}

type gameServer struct {
	proto.UnimplementedGameServiceServer
	rdb *redis.Client
}

var clients = make(map[string]*ClientInfo)
var clientMutex = sync.RWMutex{}

func main() {

	var serverAddr string
	var redisAddr string

	flag.StringVar(&serverAddr, "server", "", "Specify your server address")
	flag.StringVar(&redisAddr, "redis-server", "", "Specify your redis address.\nOn local machine it is usually localhost:6379")
	flag.Parse()

	if serverAddr == "" {
		log.Fatal("Server address required!\n")
	}

	if redisAddr == "" {
		log.Fatal("Redis server address required for room management within the server!\n",
			"On local machines it is usually localhost:6379")
	}

	listener, err := net.Listen("tcp", serverAddr)
	checkError(err)

	fmt.Println("Server started on", serverAddr)

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})

	s := grpc.NewServer()
	proto.RegisterGameServiceServer(s, &gameServer{
		rdb: rdb,
	})

	_, err = rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Unable to connect to Redis: %v", err)
	}

	go checkHeartbeats(rdb)

	// Start the room monitor in a goroutine
	go monitorRoomsStatus(rdb)

	if err := s.Serve(listener); err != nil {
		fmt.Println("Failed to serve:", err)
	}
}

func (gs *gameServer) BroadcastMessage(ctx context.Context, req *proto.MessageRequest) (*proto.MessageResponse, error) {
	broadcastMessageToClients(req.GetMessage(), ctx)
	return &proto.MessageResponse{Reply: "Message broadcasted"}, nil
}

func (gs *gameServer) SendHeartbeat(ctx context.Context, req *proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	updateHeartbeat(req.GetClientId())
	challenge := "What is 2 + 2?"
	return &proto.HeartbeatResponse{Success: true, ChallengeQuestion: challenge}, nil
}

func (gs *gameServer) SendMessageToServer(ctx context.Context, req *proto.SendMessageRequest) (*proto.SendMessageResponse, error) {
	fmt.Printf("Received message: %s\n", req.GetMessage())

	// Broadcast message to all clients
	broadcastMessageToClients(req.GetMessage(), ctx)

	return &proto.SendMessageResponse{Success: true}, nil
}

func (gs *gameServer) RegisterClient(ctx context.Context, req *proto.RegisterClientRequest) (*proto.RegisterClientResponse, error) {
	trackClient(req.GetRpcAddress())
	return &proto.RegisterClientResponse{Success: true}, nil
}

func (gs *gameServer) SendMessageToRoom(ctx context.Context, req *proto.MessageToRoomRequest) (*proto.MessageResponse, error) {

	if gs.rdb == nil {
		log.Println("gs.rdb is null!")
		return nil, errors.New("Redis client is not initialized")
	}

	// Identify the clients in the room
	roomIDStr := req.GetRoomID()
	roomID, err := strconv.ParseInt(roomIDStr, 10, 64)
	if err != nil {
		// Handle error: e.g., return an error response indicating invalid room ID format
		return nil, fmt.Errorf("Invalid room ID format: %v", err)
	}
	roomKey := fmt.Sprintf("room:%d", roomID)

	// Retrieve all rpc_addresses for the room
	rpcAddresses, err := gs.rdb.SMembers(ctx, fmt.Sprintf("%s:rpc_addresses", roomKey)).Result()
	if err != nil {
		log.Printf("Error fetching RPC addresses for room %s: %v", roomKey, err)
		return nil, err
	}

	// Check if rpcAddresses are empty (This will help debug if no rpcAddresses are found for a given room)
	if len(rpcAddresses) == 0 {
		log.Printf("No RPC addresses found for room: %s", roomKey)
		return &proto.MessageResponse{Reply: "No clients in the room"}, nil
	}

	// Broadcast message to all clients in the room
	successCount := 0
	for _, rpcAddrWithClientName := range rpcAddresses {
		rpcAddrComponents := strings.Split(rpcAddrWithClientName, ";")
		if len(rpcAddrComponents) > 0 {
			actualRpcAddr := rpcAddrComponents[0]

			// Use actualRpcAddr for further operations
			if clientInfo, exists := clients[actualRpcAddr]; exists && clientInfo.RPCClient != nil {
				_, err := clientInfo.RPCClient.BroadcastMessage(ctx, &proto.MessageRequest{Message: req.GetMessage()})
				if err != nil {
					log.Printf("Failed to send message to client at %s: %v", actualRpcAddr, err)
				} else {
					successCount++
				}
			} else {
				log.Printf("Client at %s either doesn't exist or has a nil RPCClient.", actualRpcAddr)
			}
		} else {
			log.Printf("Invalid RPC address format in room: %s", roomKey)
		}
	}

	log.Printf("Sent message to %d/%d clients in room %s", successCount, len(rpcAddresses), roomKey)

	if successCount > 0 {
		return &proto.MessageResponse{
			Success: true,
			Reply:   "Message sent to room",
		}, nil
	} else {
		return &proto.MessageResponse{
			Success: false,
			Reply:   "Failed to send message to any client in the room",
		}, nil
	}
}

func checkError(err error) {
	if err != nil {
		fmt.Println("Fatal error ", err.Error())
		os.Exit(1)
	}
}

func updateHeartbeat(clientAddr string) {
	clientMutex.Lock()
	defer clientMutex.Unlock()

	if client, exists := clients[clientAddr]; exists {
		client.LastHeartbeat = time.Now()
	}
}

func trackClient(rpcAddr string) {
	clientMutex.Lock()
	defer clientMutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, rpcAddr, grpc.WithInsecure())

	if err != nil {
		fmt.Printf("Failed to set up RPC client for address %s: %v\n", rpcAddr, err)
		return
	}

	// If a client with the exact rpcAddr exists, update its LastHeartbeat and RPCClient. Otherwise, add a new entry.
	if existingClient, exists := clients[rpcAddr]; exists {
		existingClient.RPCClient = proto.NewGameServiceClient(conn)
		existingClient.LastHeartbeat = time.Now()
	} else {
		clients[rpcAddr] = &ClientInfo{
			LastHeartbeat: time.Now(),
			RPCClient:     proto.NewGameServiceClient(conn),
		}

		data := map[string]interface{}{
			"activity_entry_date": time.Now().Format(time.RFC3339),
		}

		records := []map[string]interface{}{data}

		inputData := map[string]interface{}{
			"table_name": "user_activity",
			"records":    records,
		}

		jsonData, err := json.Marshal(inputData)
		if err != nil {
			fmt.Printf("Failed to marshal json: %v\n", err)
			return
		}

		_, err = http.Post("http://host.docker.internal:8091/database/records", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Failed to send POST request: %v\n", err)
			return
		}

		playerCountMutex.Lock()
		currentPlayers++
		playerCountMutex.Unlock()
	}
}

func reassignHostIfInactive(rdb *redis.Client, roomID int, currentHostRpcAddress string) error {
	roomKey := fmt.Sprintf("room:%d", roomID)

	// Retrieve all rpc_addresses for the room
	rpcAddresses, err := rdb.SMembers(context.Background(), fmt.Sprintf("%s:rpc_addresses", roomKey)).Result()
	if err != nil {
		return fmt.Errorf("Error fetching RPC addresses for room %s: %v", roomKey, err)
	}

	// Filter out the current host from the list of rpcAddresses
	var potentialNewHosts []string
	for _, addr := range rpcAddresses {
		if strings.Split(addr, ";")[0] != currentHostRpcAddress {
			potentialNewHosts = append(potentialNewHosts, addr)
		}
	}

	// If there are no potential new hosts, delete the room and return
	if len(potentialNewHosts) == 0 {
		return fmt.Errorf("Room %s doesn't have any player, room deletion pending...", roomKey)
	}

	// Randomly pick a new host from the list of potential new hosts
	newHostIndex := rand.Intn(len(potentialNewHosts))
	newHost := potentialNewHosts[newHostIndex]

	// Set the new host rpc address in Redis
	err = rdb.HSet(context.Background(), roomKey, "host_rpc_address", newHost).Err()
	if err != nil {
		return fmt.Errorf("Error setting new host: %v", err)
	}
	return nil
}

func challengeClient(client *ClientInfo, challengeQuestion string) bool {
	expectedResponse := "4" // This can also be stored in a map with the challenge as the key.

	response, err := client.RPCClient.SolveChallenge(context.Background(), &proto.ChallengeRequest{Question: challengeQuestion})
	if err != nil {
		log.Printf("Failed to challenge client: %v", err)
		return false
	}

	if response.Answer != expectedResponse {
		return false
	}

	return true
}

func removeClient(addr string, rdb *redis.Client, msg string) bool {
	fmt.Printf(msg)

	// Update room in Redis if required
	roomKey := getRoomKeyForClient(rdb, addr)
	if roomKey != "" {
		err := decrementRoomPlayersCount(rdb, roomKey)
		if err != nil {
			fmt.Printf("Failed to decrement player count for room %s: %v\n", roomKey, err)
		}

		// Retrieve all rpc_addresses
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		rpcAddresses, err := rdb.SMembers(ctx, fmt.Sprintf("%s:rpc_addresses", roomKey)).Result()
		if err != nil {
			fmt.Printf("Failed to retrieve rpc_addresses from room %s: %v\n", roomKey, err)
		}

		// Find and remove the rpc_address that contains addr as a substring
		var hostRemoved bool
		for _, rpcAddr := range rpcAddresses {
			if strings.Contains(rpcAddr, addr) {
				err = rdb.SRem(ctx, fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddr).Err()
				if err != nil {
					fmt.Printf("Failed to remove rpc address %s from room %s: %v\n", rpcAddr, roomKey, err)
				}
				// Check if the removed client was the host
				hostRpcAddress, _ := rdb.HGet(ctx, roomKey, "host_rpc_address").Result()
				if strings.Split(hostRpcAddress, ";")[0] == addr {
					hostRemoved = true
				}
				break // assuming there's only one matching rpcAddr
			}
		}

		// Get room id
		roomKey := getRoomKeyForClient(rdb, addr)
		var roomID int
		if roomKey != "" {
			roomIDStr := strings.Split(roomKey, ":")[1]
			roomID, err = strconv.Atoi(roomIDStr)
			if err != nil {
				// Handle error converting roomIDStr to int
				fmt.Printf("Error converting roomIDStr to int: %v\n", err)
				return true
			}
		}

		// If host was removed, reassign a new host
		if hostRemoved {
			err = reassignHostIfInactive(rdb, roomID, addr) // Adjusted this line
			if err != nil {
				fmt.Printf("Failed to reassign host for room %s: %v\n", roomKey, err)
			}
		}
	}

	delete(clients, addr)

	playerCountMutex.Lock()
	currentPlayers--
	playerCountMutex.Unlock()

	return false
}

func checkHeartbeats(rdb *redis.Client) {
	for {
		time.Sleep(heartbeatInterval)
		clientMutex.Lock()

		currentTime := time.Now()
		for addr, clientInfo := range clients {
			challengeQuestion := "What is 2 + 2?"

			switch {
			case !challengeClient(clientInfo, challengeQuestion):
				msg := fmt.Sprintf("Client failed the challenge.\n")
				res := removeClient(addr, rdb, msg)
				if res {

					continue
				}
			case currentTime.Sub(clientInfo.LastHeartbeat) > heartbeatInterval*time.Duration(missedHeartbeatsAllowed):
				msg := fmt.Sprintf("Client %s missed heartbeats. Removing from list.\n", addr)
				res := removeClient(addr, rdb, msg)
				if res {
					continue
				}
			}
		}

		clientMutex.Unlock()
	}
}

func getRoomKeyForClient(rdb *redis.Client, clientAddr string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	iter := rdb.Scan(ctx, 0, "room:*", 0).Iterator()
	for iter.Next(ctx) {
		roomKey := iter.Val()

		// Retrieve all RPC addresses with user names from the set
		rpcAddresses, err := rdb.SMembers(ctx, fmt.Sprintf("%s:rpc_addresses", roomKey)).Result()
		if err != nil {
			fmt.Printf("\nError retrieving RPC addresses for %s:rpc_addresses: %v\n", roomKey, err)
			continue // go to the next iteration
		}

		// Check if clientAddr is a substring in any of the rpc_addresses set members
		for _, addrWithUser := range rpcAddresses {
			if strings.Contains(addrWithUser, clientAddr) {
				return roomKey
			}
		}
	}

	// Check for iterator errors
	if err := iter.Err(); err != nil {
		fmt.Printf("Error retrieving keys from Redis: %v\n", err)
	}

	fmt.Printf("\nNo room key found for client: %s\n", clientAddr)
	return ""
}

func decrementRoomPlayersCount(rdb *redis.Client, roomKey string) error {
	_, err := rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Result()
	if err != nil {
		return fmt.Errorf("Error decrementing player count: %v", err)
	}
	return err
}

func broadcastMessageToClients(message string, ctx context.Context) {
	clientMutex.RLock()
	defer clientMutex.RUnlock()

	fmt.Printf("Broadcasting message: %s\n", message)
	senderAddr := ""
	p, ok := peer.FromContext(ctx)
	if ok {
		senderAddr = p.Addr.String()
	}

	for _, clientInfo := range clients {
		if clientInfo.RPCClient != nil {
			_, err := clientInfo.RPCClient.ReceiveMessageFromServer(context.Background(), &proto.MessageFromServerRequest{Message: message, SenderAddress: senderAddr})
			if err != nil {
				fmt.Printf("Failed to send message to client: %v\n", err)
			}
		}
	}
}

func monitorRoomsStatus(rdb *redis.Client) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		iter := rdb.Scan(context.Background(), 0, "room:*", 0).Iterator()
		for iter.Next(context.Background()) {
			roomKey := iter.Val()

			// Check if the key type is hash to avoid the WRONGTYPE error
			keyType, err := rdb.Type(context.Background(), roomKey).Result()
			if err != nil {
				fmt.Printf("Failed to get type for key %s: %v\n", roomKey, err)
				strings.Repeat("=", 50)
				continue
			}
			if keyType != "hash" {
				fmt.Printf("Skipping non-hash key %s\n", roomKey)
				strings.Repeat("=", 50)
				continue
			}

			currentPlayers, err := rdb.HGet(context.Background(), roomKey, "current_players").Int()
			if err != nil {
				fmt.Printf("Failed to get current_players for room %s: %v\n", roomKey, err)
				strings.Repeat("=", 50)
				continue
			}

			if currentPlayers == 0 {
				roomName, _ := rdb.HGet(context.Background(), roomKey, "room_name").Result()

				rdb.Del(context.Background(), roomKey)
				rdb.Del(context.Background(), fmt.Sprintf("room_name:%s", roomName))

				fmt.Printf("Deleted empty room %s\n", roomKey)

				fmt.Println(strings.Repeat("=", 50))
			}
		}
	}
}
