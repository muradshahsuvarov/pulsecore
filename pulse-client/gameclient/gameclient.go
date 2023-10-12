package gameclient

import (
	"errors"
	"net"
	"time"
)

type Client struct {
	ServerAddress string
	Connection    *net.UDPConn
}

func NewClient(serverAddress string) *Client {
	return &Client{
		ServerAddress: serverAddress,
	}
}

func (c *Client) Connect() error {
	addr, err := net.ResolveUDPAddr("udp", c.ServerAddress)
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}

	c.Connection = conn
	return nil
}

func (c *Client) Send(data []byte) error {
	if c.Connection == nil {
		return errors.New("client not connected")
	}
	_, err := c.Connection.Write(data)
	return err
}

func (c *Client) Receive() ([]byte, error) {
	if c.Connection == nil {
		return nil, errors.New("client not connected")
	}

	c.Connection.SetReadDeadline(time.Now().Add(5 * time.Second))

	buffer := make([]byte, 1024)
	n, _, err := c.Connection.ReadFromUDP(buffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, errors.New("read timeout")
		}
		return nil, err
	}
	return buffer[:n], nil
}

func (c *Client) Close() error {
	if c.Connection != nil {
		err := c.Connection.Close()
		c.Connection = nil
		return err
	}
	return nil
}
