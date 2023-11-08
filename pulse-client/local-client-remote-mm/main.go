package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pulsecore/proto/proto"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const (
	exitCmd           = "exit"
	heartbeatInterval = 1 * time.Second
)

type Client struct {
	proto.UnimplementedGameServiceServer
	MyAddress string
}

var CurrentRoomID int = -1

var ClientName string

var serverAddr string

var applicationId string

var MyRPCAddress string

func main() {

	u := uuid.New()
	shortUUID := u.String()[:8]
	defaultClientName := fmt.Sprintf("Player_%s", shortUUID)

	// Parsing provided terminal arguments
	// Specify a proper address of the server
	// If the server is running on Docker network, locally on your machine
	// then the ip address of the Docker host is usually 127.0.0.1
	// You can speicify it as  127.0.0.1:12345 and run the client locally.
	// In case the server is deployed in a remote cloud, then use a proper external ip and the port
	// of the service.
	flag.StringVar(&serverAddr, "server", "pulsecore_server_0:12345", "Specify server address you want to connect with")
	flag.StringVar(&applicationId, "app", "", "Specify your registered applicaiton")
	flag.StringVar(&ClientName, "name", defaultClientName, "A name for the client")
	flag.Parse()

	// Starting the client
	StartClient(serverAddr, applicationId, ClientName)
}

func StartClient(serverAddr string, applicationId string, ClientName string) {

	if applicationId == "" {
		log.Fatal("Application id can not be empty")
	}

	// Check if applicationId exists in the database
	appExists := checkAppIdExists(applicationId)
	if !appExists {
		log.Fatal("No such app identifier registered")
	}

	// Check if the provided server is associated with the applicationId
	serverExists := checkServerAssociated(applicationId, serverAddr)
	if !serverExists {
		log.Fatal("There's no such server registered with this application id")
	}

	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Client name:", ClientName)
	fmt.Println(strings.Repeat("=", 50))

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
	fmt.Println(strings.Repeat("=", 50))

	// Register the client with the dynamically allocated port
	var host string
	host = "host.docker.internal"

	rpcAddress := fmt.Sprintf("%s:%d", host, dynamicPort)

	regResp, err := registerClient(client, rpcAddress)
	if err != nil || !regResp.GetSuccess() {
		log.Fatalf("Failed to register client: %v", err)
	}
	fmt.Println("Client registered successfully!")
	fmt.Println(strings.Repeat("=", 50))

	done := make(chan bool)

	// Start a go routine to send heartbeats regularly and handle challenges
	go func() {
		for {
			time.Sleep(heartbeatInterval) // Wait for heartbeatInterval duration

			resp, err := sendHeartbeat(client, rpcAddress)
			if err != nil || !resp.GetSuccess() {
				log.Printf("Failed to send heartbeat or server rejected heartbeat: %v", err)
			}

			if resp.GetChallengeQuestion() != "" {
				if !solveChallenge(resp.GetChallengeQuestion()) {
					log.Println("Failed to solve server's challenge. Stopping heartbeats.")
					done <- true
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-done:
				log.Println("Terminating the main program due to failed challenge.")
				os.Exit(1)
			default:
				time.Sleep(time.Millisecond * 100)
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

	MyRPCAddress = rpcAddress + ";" + ClientName
	choiceHandler(reader, client, dynamicPort, rpcAddress)
}

func solveChallenge(challengeQuestion string) bool {
	if challengeQuestion == "What is 2 + 2?" {
		return true
	}
	return false
}

func checkAppIdExists(appId string) bool {
	requestData := struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}{
		TableName:  "applications",
		Conditions: map[string]interface{}{"app_identifier": appId},
	}

	resp, err := sendPostRequest("http://localhost:8091/database/records/query", requestData)
	if err != nil {
		log.Fatal("Failed to make a request to the database: ", err)
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(resp, &results); err != nil {
		log.Fatal("Failed to decode the response: ", err)
	}

	return len(results) > 0
}

func checkServerAssociated(appId, server string) bool {
	requestData := struct {
		TableName  string                 `json:"table_name"`
		Columns    []string               `json:"columns"`
		Conditions map[string]interface{} `json:"conditions"`
	}{
		TableName:  "server_addresses",
		Conditions: map[string]interface{}{"app_identifier": appId},
	}

	resp, err := sendPostRequest("http://localhost:8091/database/records/query", requestData)
	if err != nil {
		log.Fatal("Failed to make a request to the database: ", err)
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(resp, &results); err != nil {
		log.Fatal("Failed to decode the response: ", err)
	}

	for _, result := range results {
		if result["address"] == server {
			return true
		}
	}
	return false
}

func sendPostRequest(url string, data interface{}) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func choiceHandler(reader *bufio.Reader, client proto.GameServiceClient, dynamicPort int, rpcAddress string) {
	for {
		printSeparator := func() {
			fmt.Println(strings.Repeat("=", 50))
		}
		printSeparator()
		fmt.Println("1: Send a message")
		fmt.Println("2: Create a room")
		fmt.Println("3: Join a room")
		fmt.Println("4: Auto-join available room")
		fmt.Println("5: Leave a room")
		fmt.Println("6: List rooms")
		fmt.Println("7: View room details")
		fmt.Println("8: Current Room")
		fmt.Println("9: Room-specific message")
		fmt.Println("10: Change the host")
		fmt.Println("11: My RPC Address")
		fmt.Println("12: Start the game")
		fmt.Println("13: Exit")
		printSeparator()
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

			roomID, err := createRoom(roomName, rpcAddress)
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

			err = joinRoom(roomID, dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error joining room: %v", err)
			} else {
				CurrentRoomID = roomID
			}
		case "4":
			printSeparator()
			if CurrentRoomID != -1 {
				fmt.Println("You are already connected to a room. Please leave the current room before joining another.")
				continue
			}

			roomID, err := joinAvailableRoom(dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error auto-joining room: %v", err)
			} else {
				CurrentRoomID = roomID
			}
		case "5":
			printSeparator()
			err := leaveCurrentRoom(dynamicPort, rpcAddress)
			if err != nil {
				log.Printf("Error leaving room: %v", err)
			} else {
				fmt.Println("Left room successfully!")
			}
		case "6":
			printSeparator()
			rooms, err := listRooms()
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
		case "7":
			printSeparator()
			fmt.Print("Enter room ID to view details: ")
			roomIDStr, _ := reader.ReadString('\n')
			roomID, _ := strconv.Atoi(strings.TrimSpace(roomIDStr))

			err := printRoomData(roomID)
			if err != nil {
				log.Printf("Error fetching room data: %v", err)
			}
		case "8":
			printSeparator()
			err := printRoomData(CurrentRoomID)
			if err != nil {
				log.Printf("Error fetching current room data: %v", err)
			}
		case "9":
			printSeparator()
			if CurrentRoomID == -1 {
				fmt.Println("You are not connected to any room. Please join a room first before sending a room-specific message.")
				continue
			}
			printSeparator()
			fmt.Print("Enter your room-specific message: ")
			message, _ := reader.ReadString('\n')
			message = strings.TrimSpace(message)
			msgResp, err := sendRoomMessageToServer(client, CurrentRoomID, message)
			if err != nil {
				log.Printf("Failed to send room message to server due to an error: %v", err)
			} else if !msgResp.GetSuccess() {
				log.Printf("Failed to send room message to server: Message not successful.")
			} else {
				fmt.Println("Room message sent successfully!")
			}
		case "10":
			printSeparator()
			fmt.Println("Enter a new host:")
			newHostRPCAddress, _ := reader.ReadString('\n')
			err := transferHost(CurrentRoomID, MyRPCAddress, newHostRPCAddress)
			if err != nil {
				fmt.Println("Error happed during the host transfer...")
				fmt.Printf("Error meesage: %s\n", err.Error())
				continue
			}
		case "11":
			printSeparator()
			fmt.Println(MyRPCAddress)

		case "12":
			printSeparator()
			fmt.Println("Do you want to start the game?\n1 - Yes")
			option, _ := reader.ReadString('\n')
			option = strings.TrimSpace(option)
			switch option {
			case "1":
				fmt.Println("Enter the size of a flask in (width,height) format:")
				flask_size, _ := reader.ReadString('\n')
				flask_size = strings.TrimSpace(flask_size)
				pattern := `\((\d+),(\d+)\)`
				re := regexp.MustCompile(pattern)
				matches := re.FindAllStringSubmatch(flask_size, -1)
				if len(matches) != 1 {
					fmt.Println("Incorrect size format")
					continue
				}
				for _, match := range matches {
					fmt.Printf("Width: %s, Height: %s\n", match[1], match[2])
				}

			case "0":
				continue
			default:
				fmt.Println("There is no such as option")
			}

		case "13":
			printSeparator()
			fmt.Println("Exiting...")
			return
		default:
			printSeparator()
			fmt.Println("Invalid choice. Please try again.")
		}
	}
}

func sendRoomMessageToServer(client proto.GameServiceClient, roomID int, message string) (*proto.MessageResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return client.SendMessageToRoom(ctx, &proto.MessageToRoomRequest{
		RoomID:  strconv.Itoa(roomID),
		Message: message,
	})
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
		fmt.Println("\n")
		fmt.Println(strings.Repeat("=", 50))
		fmt.Printf("\nReceived message from another client: %s\n", req.Message)
	}
	fmt.Println("\n")
	fmt.Println(strings.Repeat("=", 50))
	return &proto.MessageFromServerResponse{Success: true}, nil
}

func (c *Client) SolveChallenge(ctx context.Context, req *proto.ChallengeRequest) (*proto.ChallengeResponse, error) {
	answer := "4"
	return &proto.ChallengeResponse{Answer: answer}, nil
}

func createRoom(roomName string, rpcAddress string) (int, error) {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/create-room")
	roomNameFormat := fmt.Sprintf("room_name:%s", roomName)

	payload := struct {
		RoomName      string `json:"roomName"`
		ServerAddress string `json:"serverAddress"`
		RpcAddress    string `json:"rpcAddrerss"`
		ClientName    string `json:"clientName"`
	}{
		RoomName:      roomNameFormat,
		ServerAddress: serverAddr,
		RpcAddress:    rpcAddress,
		ClientName:    ClientName,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return -1, fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return -1, fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())

	}

	defer res.Body.Close()

	var room_id int

	if res.StatusCode == http.StatusCreated {
		res_body := struct {
			Message string `json:"message"`
			RoomId  int    `json:"roomId"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return -1, fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return -1, fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}

		room_id = res_body.RoomId
	}

	return room_id, nil
}

// Transfer the host role to another member
func transferHost(roomID int, currentRpcAddress string, newHostRpcAddress string) error {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/transfer-host")

	payload := struct {
		RoomID            int    `json:"roomID"`
		CurrentRPCAddress string `json:"currentRpcAddress"`
		NewHostRPCAddress string `json:"newHostRpcAddress"`
	}{
		RoomID:            roomID,
		CurrentRPCAddress: currentRpcAddress,
		NewHostRPCAddress: newHostRpcAddress,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())

	}

	defer res.Body.Close()

	var response struct {
		Message string `json:"message"`
		OldHost string `json:"oldhost"`
		NewHost string `json:"newhost"`
		RoomID  int    `json:"roomid"`
		RoomKey string `json:"roomkey"`
	}

	if res.StatusCode == http.StatusCreated {
		res_body := struct {
			Message string `json:"message"`
			OldHost string `json:"oldhost"`
			NewHost string `json:"newhost"`
			RoomID  int    `json:"roomid"`
			RoomKey string `json:"roomkey"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}

		response = res_body
	}

	fmt.Println(response)

	return nil
}

func joinAvailableRoom(dynamicPort int, rpcAddress string) (int, error) {

	req_url := fmt.Sprintf("http://host.docker.internal:8096/")

	payload := struct {
		ClientName string `json:"clientName"`
		ClientID   int    `json:"clientID"`
		RpcAddress string `json:"rpcAddress"`
	}{
		ClientName: ClientName,
		ClientID:   dynamicPort,
		RpcAddress: MyRPCAddress,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return -1, fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))

	if err != nil {
		return -1, fmt.Errorf("Error during the request processing. Error: %s", err.Error())
	}

	defer res.Body.Close()

	if err != nil {
		return -1, fmt.Errorf("Error happened during requestion processing. Error message: %s", err.Error())
	}

	var roomId int

	if res.StatusCode == http.StatusOK {
		res_body := struct {
			Message string `json:"message"`
			RoomId  int    `json:"roomId"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return -1, fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return -1, fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}

		roomId = res_body.RoomId
	}

	return roomId, nil
}

func joinRoom(roomID int, clientID int, rpcAddress string) error {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/join-room")

	payload := struct {
		RoomID     int    `json:"roomID"`
		ClientID   int    `json:"clientID"`
		RpcAddress string `json:"rpcAddrerss"`
		ClientName string `json:"clientName"`
	}{
		RoomID:     roomID,
		ClientID:   clientID,
		RpcAddress: rpcAddress,
		ClientName: ClientName,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())

	}

	defer res.Body.Close()

	if res.StatusCode == http.StatusCreated {
		res_body := struct {
			Message string `json:"message"`
			RoomId  int    `json:"roomId"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}
	}

	return nil
}

func leaveRoom(roomID int) error {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/leave-room")

	payload := struct {
		RoomID int `json:"roomID"`
	}{
		RoomID: roomID,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())

	}

	defer res.Body.Close()

	if res.StatusCode == http.StatusAccepted {
		res_body := struct {
			Message string `json:"message"`
			RoomId  int    `json:"roomId"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}
	}

	return nil

}

func leaveCurrentRoom(clientID int, rpcAddress string) error {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/leave-current-room")

	payload := struct {
		CurrentRoomId int    `json:"currentRoomId"`
		ClientID      int    `json:"clientID"`
		ClientName    string `json:"clientName"`
		RpcAddress    string `json:"rpcAddress"`
	}{
		CurrentRoomId: CurrentRoomID,
		ClientID:      clientID,
		ClientName:    ClientName,
		RpcAddress:    rpcAddress,
	}

	data, err := json.Marshal(payload)

	if err != nil {
		return fmt.Errorf("Error happened during marshelling the payload. Error message: %s", err.Error())
	}

	res, err := http.Post(req_url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		CurrentRoomID = -1
		return fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())

	}

	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		res_body := struct {
			Message string `json:"message"`
			RoomId  int    `json:"roomID"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}
	}

	return nil
}

func listRooms() ([]map[string]string, error) {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/listrooms")

	res, err := http.Get(req_url)
	if err != nil {
		fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())
	}

	defer res.Body.Close()

	var rooms []map[string]string

	if res.StatusCode == http.StatusOK {
		res_body := struct {
			Rooms []map[string]string `json:"rooms"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}

		rooms = res_body.Rooms
	}

	return rooms, err

}

func printRoomData(roomID int) error {

	req_url := fmt.Sprintf("http://localhost:12354/matchmaking/roomdetails/%d", roomID)

	res, err := http.Get(req_url)
	if err != nil {
		return fmt.Errorf("Error happened during room creating request. Error Message: %s", err.Error())
	}

	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		res_body := struct {
			RoomData map[string]string `json:"roomData"`
			RpcNames []string          `json:"rpcNames"`
		}{}

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("Error happened during the response body reading. Error message: %s", err.Error())
		}

		err = json.Unmarshal(body, &res_body)

		if err != nil {
			return fmt.Errorf("Error happened during unmarshelling the response body. Error message: %s", err.Error())
		}

		fmt.Println(res_body)
	}

	return err
}

// ---------------ROOM MANAGEMENT-----------------------------

func (gc *Client) BroadcastMessage(ctx context.Context, req *proto.MessageRequest) (*proto.MessageResponse, error) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("Received message:", req.GetMessage())
	return &proto.MessageResponse{Reply: "Received the message"}, nil
}
