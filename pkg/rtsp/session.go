package rtsp

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

// This will hold parsed Transport header fields
type transportParams struct {
	UDP         bool
	ClientPorts []int
	ClientAddr  *net.UDPAddr
	Interleaved [2]byte
}

type session struct {
	URI       string
	transport transportParams
	udpRTP    *net.UDPConn
	udpRTCP   *net.UDPConn
	tcpConn   net.Conn
}

var (
	sessions   = make(map[string]*session)
	sessionsMu sync.Mutex
)

// function to allocates transport and registers a new session
func setupSession(req *RTSPRequest, conn net.Conn) string {
	tp := parseTransport(req.Headers["Transport"])
	sid := fmt.Sprintf("%08x", rand.Int31())

	sess := &session{URI: req.URI, transport: tp}

	if tp.UDP {
		rtpConn, rtcpConn, err := openUDPSockets()
		if err != nil {
			fmt.Println("[ERROR] Error while open UDP sockets: ", err)
		}
		sess.udpRTP, sess.udpRTCP = rtpConn, rtcpConn
	} else {
		sess.tcpConn = conn
	}

	sessionsMu.Lock()
	sessions[sid] = sess
	sessionsMu.Unlock()
	return sid
}

// function to clean up sockets and removes the session
func teardownSession(sid string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if s, ok := sessions[sid]; ok {
		if s.udpRTP != nil {
			s.udpRTP.Close()
			s.udpRTCP.Close()
		}

		delete(sessions, sid)
	}
}

// startRTPStreaming begins sending RTP packets for the session
func startRTPStreaming(sid string) {
	sessionsMu.Lock()
	sess := sessions[sid]
	sessionsMu.Unlock()

	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
		for range ticker.C {
			packet := buildRTPPacket()

			if sess.transport.UDP {
				sess.udpRTP.WriteTo(packet, sess.transport.ClientAddr)
			} else {
				sendInterleaved(sess.tcpConn, packet, 0)
			}
		}
	}()
}
