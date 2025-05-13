package main

import (
	"log"
	"net"

	"github.com/HOTSONHONET/cam-feed/pkg/rtsp"
)

// Initializing TCP listener and dispatches each connection
func main() {
	ln, err := net.Listen("tcp", ":8554")
	if err != nil {
		log.Fatal(err)
	}

	defer ln.Close()
	log.Println("[INFO] RTSP server listening on :8554")

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println("[ERROR] accept: ", err)
			continue
		}

		go rtsp.HandleConn(conn)
	}
}
