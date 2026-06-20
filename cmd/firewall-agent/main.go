package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type request struct {
	Action         string `json:"action"`
	IP             string `json:"ip,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

type response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type agent struct {
	chain       string
	ipset       string
	ipset6      string
	ipsetBin    string
	iptablesBin string
	ip6tableBin string
	allowlist   []*net.IPNet
}

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("firewall-agent must run as root")
	}
	allowlist, err := parseNetworks(env("FIREWALL_IP_ALLOWLIST", "127.0.0.0/8,::1/128"))
	if err != nil {
		log.Fatalf("invalid FIREWALL_IP_ALLOWLIST: %v", err)
	}
	a := &agent{
		chain:       env("FIREWALL_CHAIN", "INPUT"),
		ipset:       env("FIREWALL_IPSET_V4", "logs_dashboard_block_v4"),
		ipset6:      env("FIREWALL_IPSET_V6", "logs_dashboard_block_v6"),
		ipsetBin:    env("IPSET_BIN", "/usr/sbin/ipset"),
		iptablesBin: env("IPTABLES_BIN", "/usr/sbin/iptables"),
		ip6tableBin: env("IP6TABLES_BIN", "/usr/sbin/ip6tables"),
		allowlist:   allowlist,
	}
	if err := a.ensureFirewall(); err != nil {
		log.Fatalf("initialize firewall: %v", err)
	}
	socketPath := env("FIREWALL_AGENT_SOCKET", "/run/logs-dashboard/firewall.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0750); err != nil {
		log.Fatal(err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Fatal(err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		_ = listener.Close()
	}()
	log.Printf("firewall agent listening on %s using chain %s", socketPath, a.chain)
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		go a.handle(connection)
	}
}

func (a *agent) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(bufio.NewReader(connection))
	decoder.DisallowUnknownFields()
	var req request
	if err := decoder.Decode(&req); err != nil {
		_ = json.NewEncoder(connection).Encode(response{OK: false, Message: "invalid request"})
		return
	}
	var err error
	switch req.Action {
	case "status":
		err = a.ensureFirewall()
	case "block", "unblock":
		ip := net.ParseIP(strings.TrimSpace(req.IP))
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
			err = errors.New("invalid or protected IP address")
		} else if req.Action == "block" && containsIP(a.allowlist, ip) {
			err = errors.New("IP address is protected by firewall allowlist")
		} else if req.TimeoutSeconds < 0 || req.TimeoutSeconds > int64((365*24*time.Hour)/time.Second) {
			err = errors.New("invalid timeout")
		} else if req.Action == "block" {
			err = a.block(ip, req.TimeoutSeconds)
		} else {
			err = a.unblock(ip)
		}
	default:
		err = errors.New("unsupported action")
	}
	if err != nil {
		log.Printf("firewall action=%s ip=%s failed: %v", req.Action, req.IP, err)
		_ = json.NewEncoder(connection).Encode(response{OK: false, Message: err.Error()})
		return
	}
	log.Printf("firewall action=%s ip=%s succeeded", req.Action, req.IP)
	_ = json.NewEncoder(connection).Encode(response{OK: true})
}

func (a *agent) ensureFirewall() error {
	if err := run(a.ipsetBin, "create", a.ipset, "hash:ip", "family", "inet", "timeout", "0", "-exist"); err != nil {
		return err
	}
	if err := run(a.ipsetBin, "create", a.ipset6, "hash:ip", "family", "inet6", "timeout", "0", "-exist"); err != nil {
		return err
	}
	if err := ensureRule(a.iptablesBin, a.chain, a.ipset); err != nil {
		return err
	}
	return ensureRule(a.ip6tableBin, a.chain, a.ipset6)
}

func ensureRule(binary, chain, set string) error {
	args := []string{"-C", chain, "-m", "set", "--match-set", set, "src", "-j", "DROP"}
	if exec.Command(binary, args...).Run() == nil {
		return nil
	}
	return run(binary, "-I", chain, "1", "-m", "set", "--match-set", set, "src", "-j", "DROP")
}

func (a *agent) block(ip net.IP, timeout int64) error {
	set := a.ipset6
	if ip.To4() != nil {
		set = a.ipset
	}
	args := []string{"add", set, ip.String()}
	if timeout > 0 {
		args = append(args, "timeout", strconv.FormatInt(timeout, 10))
	}
	args = append(args, "-exist")
	return run(a.ipsetBin, args...)
}

func (a *agent) unblock(ip net.IP) error {
	set := a.ipset6
	if ip.To4() != nil {
		set = a.ipset
	}
	command := exec.Command(a.ipsetBin, "del", set, ip.String())
	if output, err := command.CombinedOutput(); err != nil && !strings.Contains(string(output), "is NOT in set") {
		return fmt.Errorf("%s: %w: %s", a.ipsetBin, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func run(binary string, args ...string) error {
	output, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", binary, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseNetworks(value string) ([]*net.IPNet, error) {
	var result []*net.IPNet
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, err
		}
		result = append(result, network)
	}
	return result, nil
}

func containsIP(networks []*net.IPNet, ip net.IP) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
