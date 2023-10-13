package main

import (
	"fmt"
	"io"
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
var bufferPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024)
	},
}

func main() {
	serverAddr := "localhost:12345"
	listener, err := net.Listen("tcp", serverAddr)
	checkError(err)
	defer listener.Close()

	fmt.Println("Server started on", serverAddr)

	go checkHeartbeats()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting TCP connection:", err)
			continue
		}

		go handleTCPConnection(conn)
	}
}

func handleTCPConnection(conn net.Conn) {
	defer conn.Close()

	buffer := bufferPool.Get().([]byte)
	defer bufferPool.Put(buffer)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				fmt.Println("Error reading from TCP connection:", err)
			}
			return
		}

		message := string(buffer[:n])
		handleMessage(message, conn)
	}
}

func handleMessage(message string, conn net.Conn) {
	addr := conn.RemoteAddr().String()

	if message == "heartbeat" {
		updateHeartbeat(addr)
	} else if strings.HasPrefix(message, "RPC_ADDRESS:") {
		rpcAddr := strings.TrimPrefix(message, "RPC_ADDRESS:")
		trackClient(addr, rpcAddr)
	} else {
		fmt.Printf("Received message '%s' from %s\n", message, addr)
		broadcastMessageToClients(addr, message)
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

func trackClient(tcpAddr string, rpcAddr string) {
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

	clients[tcpAddr] = &ClientInfo{
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
