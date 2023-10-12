package main

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"time"
)

const heartbeatInterval = 10 * time.Second
const missedHeartbeatsAllowed = 3

type ClientInfo struct {
	LastHeartbeat time.Time
	RPCClient     *rpc.Client
	RPCAddress    string
}

var clients = make(map[string]*ClientInfo)
var clientMutex = sync.RWMutex{}

func main() {
	serverAddr := "localhost:12345"
	udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
	checkError(err)

	conn, err := net.ListenUDP("udp", udpAddr)
	checkError(err)
	defer conn.Close()

	fmt.Println("Server started on", serverAddr)

	buffer := make([]byte, 1024)

	go checkHeartbeats()

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading from UDP connection:", err)
			continue
		}

		message := string(buffer[:n])
		if message == "heartbeat" {
			updateHeartbeat(addr.String())
		} else if strings.HasPrefix(message, "RPC_ADDRESS:") {
			rpcAddr := strings.TrimPrefix(message, "RPC_ADDRESS:")
			trackClient(addr.String(), rpcAddr)
		} else {
			fmt.Printf("Received message '%s' from %s\n", message, addr)
			broadcastMessageToClients(addr.String(), message)
		}
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

func trackClient(udpAddr string, rpcAddr string) {
	clientMutex.Lock()
	defer clientMutex.Unlock()

	var client *rpc.Client
	var err error
	for i := 0; i < 3; i++ { // Try connecting 3 times
		client, err = rpc.Dial("tcp", rpcAddr)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second) // Wait for 2 seconds before the next try
	}

	if err != nil {
		fmt.Println("Error connecting to client's RPC server after multiple attempts:", err)
		return
	}

	// This ensures that if the client exists, it's updated with the new RPC client and address
	clients[udpAddr] = &ClientInfo{
		LastHeartbeat: time.Now(),
		RPCClient:     client,
		RPCAddress:    rpcAddr,
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
				clientInfo.RPCClient.Close()
				delete(clients, addr)
			}
		}

		clientMutex.Unlock()
	}
}

func broadcastMessageToClients(senderAddr string, message string) {
	clientMutex.RLock()
	defer clientMutex.RUnlock()

	if message == "heartbeat" { // do not broadcast heartbeat messages
		return
	}

	for addr, clientInfo := range clients {
		if addr != senderAddr {
			var reply string
			err := clientInfo.RPCClient.Call("MessageService.BroadcastMessage", &message, &reply)
			if err != nil {
				fmt.Printf("Error broadcasting to client %s: %s\n", addr, err)
			}
		}
	}
}
