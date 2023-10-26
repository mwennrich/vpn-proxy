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

func handleHTTPProxyClient(clientConn net.Conn, logger *slog.Logger) {

	defer clientConn.Close()

	var url, protocol, scheme, verb, requestLine, host, path string
	vpn := false
	header := []string{}

	start := time.Now()
	reader := bufio.NewReader(clientConn)

	request, err := reader.ReadString('\n')
	requestParts := strings.Fields(request)
	if len(requestParts) == 3 && strings.HasPrefix(requestParts[2], "HTTP/") {
		verb = requestParts[0]
		url = requestParts[1]
		protocol = requestParts[2]
		requestLine = request
	} else {
		logger.Error("Error parsing request", "error", err, "line", request)
		return
	}

	switch verb {
	case "CONNECT":
		scheme = "https"
		host = url
	default:
		scheme = "http"
		host = strings.Split(url, "/")[2]
		if !strings.Contains(host, ":") {
			host = host + ":80"
		}
		path = "/" + strings.Join(strings.Split(url, "/")[3:], "/")
	}

	// get request header
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Error("Error reading request line", "error", err)
			return
		}
		logger.Debug("got header", "line", line)

		if strings.HasPrefix(line, "Reversed-VPN") {
			vpn = true
		}
		if line == "\r\n" {
			break
		}
		header = append(header, line)
	}
	logger.Info("new connection", "from", clientConn.RemoteAddr().String(), "to", url, "proto", protocol, "scheme", scheme, "verb", verb)

	// connect to target
	serverAddr, err := net.ResolveTCPAddr("tcp", host)
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

	if scheme == "https" {
		// for https send 200 OK back to the client
		clientConn.Write([]byte(protocol + " 200 OK\r\nConnection: Keep-Alive\r\n\r\n"))
	} else {
		// for http, sent http request to target
		serverConn.Write([]byte(verb + " " + path + " " + protocol + "\r\n"))
		logger.Debug("sending request", "line", verb+" "+path+" "+protocol)
	}

	// special handling for VPN
	// forward request to target as another proxy request
	if vpn {
		logger.Info("proxying VPN connection", "from", clientConn.RemoteAddr().String(), "to", url)
		serverConn.Write([]byte(requestLine))
	}

	// if http or vpn special case, send all headers to target
	if vpn || scheme == "http" {
		for _, v := range header {
			serverConn.Write([]byte(v))
			logger.Debug("sending header", "line", v)
		}
		serverConn.Write([]byte("\r\n"))
	}

	// copy data between client and target
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
	logger.Info("connection closed", "from", clientConn.RemoteAddr().String(), "to", url, "bytesReceivedFromClient", bytesReceivedFromClient, "bytesSentToClient", bytesSentToClient, "duration", time.Since(start).Truncate(time.Millisecond).String())
}

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// Level: slog.LevelDebug,
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
		go handleHTTPProxyClient(clientConn, logger)
	}
}
