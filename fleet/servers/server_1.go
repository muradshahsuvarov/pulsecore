package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"pulsecore/proto/proto"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

var currentPlayers int
var playerCountMutex = sync.Mutex{}

const (
	serverAddr              = "localhost:12345"
	heartbeatInterval       = 10 * time.Second
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
	listener, err := net.Listen("tcp", serverAddr)
	checkError(err)

	s := grpc.NewServer()
	proto.RegisterGameServiceServer(s, &gameServer{})

	fmt.Println("Server started on", serverAddr)

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
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

	roomKey, err := rdb.Get(context.Background(), "client:"+clientAddr).Result()
	if err != nil {
		return ""
	}
	return roomKey
}

func decrementRoomPlayersCount(rdb *redis.Client, roomKey string) error {
	err := rdb.HIncrBy(context.Background(), roomKey, "current_players", -1).Err()
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
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		iter := rdb.Scan(context.Background(), 0, "room:*", 0).Iterator()
		for iter.Next(context.Background()) {
			roomKey := iter.Val()

			currentPlayers, err := rdb.HGet(context.Background(), roomKey, "current_players").Int()
			if err != nil {
				fmt.Printf("Failed to get current_players for room %s: %v\n", roomKey, err)
				continue
			}

			if currentPlayers == 0 {
				roomName, _ := rdb.HGet(context.Background(), roomKey, "room_name").Result()

				rdb.Del(context.Background(), roomKey)
				rdb.Del(context.Background(), fmt.Sprintf("room_name:%s", roomName))

				fmt.Printf("Deleted empty room %s\n", roomKey)
			}
		}
	}
}
