package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

type FirewallClient struct {
	socketPath string
	timeout    time.Duration
}

type firewallRequest struct {
	Action         string `json:"action"`
	IP             string `json:"ip,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

type firewallResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func NewFirewallClient(socketPath string) *FirewallClient {
	return &FirewallClient{socketPath: socketPath, timeout: 5 * time.Second}
}

func (c *FirewallClient) Available() bool {
	return c != nil && c.socketPath != ""
}

func (c *FirewallClient) Status() error {
	_, err := c.request(firewallRequest{Action: "status"})
	return err
}

func (c *FirewallClient) Block(ip string, timeoutSeconds int64) error {
	_, err := c.request(firewallRequest{Action: "block", IP: ip, TimeoutSeconds: timeoutSeconds})
	return err
}

func (c *FirewallClient) Unblock(ip string) error {
	_, err := c.request(firewallRequest{Action: "unblock", IP: ip})
	return err
}

func (c *FirewallClient) request(request firewallRequest) (firewallResponse, error) {
	if !c.Available() {
		return firewallResponse{}, errors.New("firewall agent socket is not configured")
	}
	connection, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return firewallResponse{}, fmt.Errorf("connect firewall agent: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(c.timeout))
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return firewallResponse{}, fmt.Errorf("send firewall request: %w", err)
	}
	var response firewallResponse
	if err := json.NewDecoder(bufio.NewReader(connection)).Decode(&response); err != nil {
		return firewallResponse{}, fmt.Errorf("read firewall response: %w", err)
	}
	if !response.OK {
		if response.Message == "" {
			response.Message = "firewall operation failed"
		}
		return response, errors.New(response.Message)
	}
	return response, nil
}
