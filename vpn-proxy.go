package main

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

func handleHTTPSProxyClient(clientConn net.Conn, logger *slog.Logger) {

	defer clientConn.Close()

	start := time.Now()
	reader := bufio.NewReader(clientConn)

	hostPort := ""
	vpn := false
	requestLine := ""
	protocol := ""
	header := []string{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Error("Error reading request line", "error", err)
			return
		}
		logger.Debug("got", "line", line)
		parts := strings.Fields(line)
		if len(parts) == 3 && parts[0] == "CONNECT" {
			hostPort = parts[1]
			protocol = parts[2]
			requestLine = line
			continue
		}
		if strings.HasPrefix(line, "Reversed-VPN") {
			vpn = true
		}
		if line == "\r\n" {
			break
		}
		header = append(header, line)
	}
	logger.Info("new connection", "from", clientConn.RemoteAddr().String(), "to", hostPort, "proto", protocol)

	serverAddr, err := net.ResolveTCPAddr("tcp", hostPort)
	if err != nil {
		logger.Error("Error resolving target address", "error", err)
		return
	}

	serverConn, err := net.DialTCP("tcp", nil, serverAddr)
	if err != nil {
		logger.Error("Error connecting to target", "error", err)
		return
	}
	serverConn.SetKeepAlive(true)
	defer serverConn.Close()

	clientConn.Write([]byte(protocol + " 200 OK\r\nConnection: Keep-Alive\r\n\r\n"))

	if vpn {
		logger.Info("proxying VPN connection", "from", clientConn.RemoteAddr().String(), "to", hostPort)
		serverConn.Write([]byte(requestLine))
		for _, v := range header {
			serverConn.Write([]byte(v))
		}
		serverConn.Write([]byte("\r\n"))
	}

	var bytesReceivedFromClient int64
	go func(bytesReceivedFromClient *int64) {
		w, err := io.Copy(serverConn, clientConn)
		if err != nil {
			logger.Debug("Error copying from client to target", "error", err)
		}
		*bytesReceivedFromClient = w
	}(&bytesReceivedFromClient)

	bytesSentToClient, err := io.Copy(clientConn, serverConn)
	if err != nil {
		logger.Debug("Error copying from target to client", "error", err)
	}
	clientConn.Close()
	serverConn.Close()
	logger.Info("connection closed", "from", clientConn.RemoteAddr().String(), "to", hostPort, "bytesReceivedFromClient", bytesReceivedFromClient, "bytesSentToClient", bytesSentToClient, "duration", time.Since(start).Truncate(time.Millisecond).String())
}

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	listenAddress := ":8080"
	listenAddr, err := net.ResolveTCPAddr("tcp", listenAddress)
	if err != nil {
		logger.Error("Error resolving listen address", "listenAddress", listenAddress, "error", err)
		return
	}

	listen, err := net.ListenTCP("tcp", listenAddr)
	if err != nil {
		logger.Error("Error listen on", "listenAddress", listenAddress, "error", err)
		return
	}
	defer listen.Close()

	logger.Info("Proxy server listening on " + listenAddress)

	for {
		clientConn, err := listen.AcceptTCP()
		if err != nil {
			logger.Error("Error accepting connection", "error", err)
			continue
		}
		clientConn.SetKeepAlive(true)
		go handleHTTPSProxyClient(clientConn, logger)
	}
}
