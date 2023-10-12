package main

import (
	"bufio"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"pulsecore/pulse-client/gameclient"
	"time"
)

// MessageService is the RPC service that the client exposes
type MessageService struct {
	client *gameclient.Client
}

const (
	heartbeatInterval = 10 * time.Second
	rpcPort           = "localhost:12346"
)

func main() {
	client := gameclient.NewClient("localhost:12345")
	connectToServer(client)

	// Start receiving data asynchronously
	go asyncDataReceiver(client)

	// Start RPC server in the background for this client
	go startRPCServer(client)

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("Enter a message to send to the server (type 'exit' to quit):")
		text, _ := reader.ReadString('\n')

		if text == "exit\n" {
			break
		}

		err := client.Send([]byte(text))
		if err != nil {
			fmt.Println("Error sending data:", err)
			fmt.Println("Attempting to reconnect...")
			connectToServer(client)
		}
	}
}

func (ms *MessageService) BroadcastMessage(message *string, reply *string) error {
	fmt.Println("Received broadcast:", *message)
	*reply = "Message received"
	return nil
}

func asyncDataReceiver(client *gameclient.Client) {
	for {
		data, err := client.Receive()
		if err != nil {
			// Check if error is a read timeout
			if err.Error() == "read timeout" {
				continue // continue listening without trying to reconnect
			}

			fmt.Println("Error receiving data:", err)
			fmt.Println("Attempting to reconnect...")
			connectToServer(client)
			continue
		}

		fmt.Println("Received from server:", string(data))
	}
}

func connectToServer(client *gameclient.Client) {
	for {
		err := client.Connect()
		if err != nil {
			fmt.Println("Error connecting:", err)
			fmt.Println("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		} else {
			fmt.Println("Connected successfully!")

			// Start sending heartbeats in the background
			go sendHeartbeats(client)

			break
		}
	}
}

func sendHeartbeats(client *gameclient.Client) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			client.Send([]byte("heartbeat"))
		}
	}
}

func startRPCServer(client *gameclient.Client) {
	msgService := &MessageService{client: client}
	rpc.Register(msgService)

	listener, err := net.Listen("tcp", "localhost:0") // 0 allows the OS to pick an available port
	if err != nil {
		fmt.Println("Error starting RPC server:", err)
		return
	}
	defer listener.Close()

	// After the listener is started, get the actual port number
	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("RPC Server started on port %d\n", actualPort)

	// Then update the RPC_ADDRESS line in the connectToServer function
	client.Send([]byte(fmt.Sprintf("RPC_ADDRESS:localhost:%d", actualPort)))

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting RPC connection:", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}
