package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"pulsecore/proto/proto"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

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
	go checkHeartbeats()

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
	}
}

func checkHeartbeats() {
	for {
		time.Sleep(heartbeatInterval)
		clientMutex.Lock()

		currentTime := time.Now()
		for addr, clientInfo := range clients {
			if currentTime.Sub(clientInfo.LastHeartbeat) > heartbeatInterval*time.Duration(missedHeartbeatsAllowed) {
				fmt.Printf("Client %s missed heartbeats. Removing from list.\n", addr)
				delete(clients, addr)
			}
		}

		clientMutex.Unlock()
	}
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
