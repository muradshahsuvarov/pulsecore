package gameclient

import (
	"context"
	"pulsecore/proto/proto"
	"time"

	"google.golang.org/grpc"
)

type Client struct {
	Address   string
	RPCClient proto.GameServiceClient
	Id        string // assuming ID is a string, adjust type if necessary
	RPCConn   *grpc.ClientConn
}

func NewClient(address string) *Client {
	return &Client{
		Address: address,
	}
}

// Connect establishes a gRPC connection to the provided address
func (c *Client) Connect() error {
	conn, err := grpc.Dial(c.Address, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	if err != nil {
		return err
	}
	c.RPCConn = conn
	c.RPCClient = proto.NewGameServiceClient(conn)
	return nil
}

// You can add more methods here that correspond to the RPC methods you've defined. For example:

func (c *Client) SendMessageToServer(message string) (*proto.SendMessageResponse, error) {
	return c.RPCClient.SendMessageToServer(context.Background(), &proto.SendMessageRequest{Message: message})
}

func (c *Client) SendHeartbeat() (*proto.HeartbeatResponse, error) {
	return c.RPCClient.SendHeartbeat(context.Background(), &proto.HeartbeatRequest{ClientId: c.Id})
}

func (c *Client) RegisterClient(rpcAddress string) (*proto.RegisterClientResponse, error) {
	return c.RPCClient.RegisterClient(context.Background(), &proto.RegisterClientRequest{RpcAddress: rpcAddress})
}

func (c *Client) Close() error {
	if c.RPCConn != nil {
		return c.RPCConn.Close()
	}
	return nil
}
