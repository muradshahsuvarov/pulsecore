package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"pulsecore/proto/proto"

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

	// Send messages in an infinite loop until the user types "exit"
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter your message (or 'exit' to quit): ")
		message, _ := reader.ReadString('\n')
		message = strings.TrimSpace(message) // remove any trailing newline

		if message == exitCmd {
			fmt.Println("Exiting...")
			break
		}

		msgResp, err := sendMessageToServer(client, message)
		if err != nil || !msgResp.GetSuccess() {
			log.Fatalf("Failed to send message to server: %v", err)
		}
		fmt.Println("Message sent successfully!")
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
