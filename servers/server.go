package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"pulsecore/proto/proto"
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

	s := grpc.NewServer()
	proto.RegisterGameServiceServer(s, &gameServer{})

	fmt.Println("Server started on", serverAddr)

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
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
	return &proto.HeartbeatResponse{Success: true}, nil
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

		playerCountMutex.Lock()
		currentPlayers++
		playerCountMutex.Unlock()
	}
}

func checkHeartbeats(rdb *redis.Client) {
	for {
		time.Sleep(heartbeatInterval)
		clientMutex.Lock()

		currentTime := time.Now()
		for addr, clientInfo := range clients {
			if currentTime.Sub(clientInfo.LastHeartbeat) > heartbeatInterval*time.Duration(missedHeartbeatsAllowed) {
				fmt.Printf("Client %s missed heartbeats. Removing from list.\n", addr)

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
					for _, rpcAddr := range rpcAddresses {
						if strings.Contains(rpcAddr, addr) {
							err = rdb.SRem(ctx, fmt.Sprintf("%s:rpc_addresses", roomKey), rpcAddr).Err()
							if err != nil {
								fmt.Printf("Failed to remove rpc address %s from room %s: %v\n", rpcAddr, roomKey, err)
							}
							break // assuming there's only one matching rpcAddr
						}
					}
				}

				delete(clients, addr)

				playerCountMutex.Lock()
				currentPlayers--
				playerCountMutex.Unlock()
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
			fmt.Printf("Error retrieving RPC addresses for %s:rpc_addresses: %v\n", roomKey, err)
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

	// Not found
	fmt.Printf("No room key found for client: %s\n", clientAddr)
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
