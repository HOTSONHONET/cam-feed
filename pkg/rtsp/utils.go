package rtsp

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

// func to parse transport header into transportParams
func parseTransport(header string) (tp transportParams) {
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "client_port=") {
			tp.UDP = true
			ports := strings.Split(strings.TrimPrefix(part, "client_port="), "-")
			if len(ports) == 2 {
				p1, _ := strconv.Atoi(ports[0])
				p2, _ := strconv.Atoi(ports[1])
				tp.ClientPorts = []int{p1, p2}
			}
		}

		if strings.HasPrefix(part, "interleaved=") {
			vals := strings.Split(strings.TrimPrefix(part, "interleaved="), "-")
			if len(vals) == 2 {
				c1, _ := strconv.Atoi(vals[0])
				c2, _ := strconv.Atoi(vals[1])
				tp.Interleaved = [2]byte{byte(c1), byte(c2)}
			}
		}
	}

	return tp

}

// func to open local UDP sockets for RTP and RTCP
func openUDPSockets() (*net.UDPConn, *net.UDPConn, error) {
	rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}

	rtcpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		rtpConn.Close()
		return nil, nil, err
	}

	return rtpConn, rtcpConn, nil
}

// function to generate a minimal SDP description for the stream
func generateSDP(uri string) string {
	return fmt.Sprintf(`v=0
		" +
        "o=- 0 0 IN IP4 0.0.0.0
" +
        "s=Go RTSP Server
" +
        "c=IN IP4 0.0.0.0
" +
        "t=0 0
" +
        "m=video 0 RTP/AVP 96
" +
        "a=rtpmap:96 H264/90000
" +
        "a=control:trackID=0
	`)
}

// func to create Transport response header for SETUP
func transportResponse(sess *session) string {
	tp := sess.transport
	if tp.UDP {
		// server ports
		localRTP := sess.udpRTP.LocalAddr().(*net.UDPAddr).Port
		localRTCP := sess.udpRTCP.LocalAddr().(*net.UDPAddr).Port

		return fmt.Sprintf(
			"RTP/AVP;unicast;client_port=%d-%d;server_port=%d-%d",
			tp.ClientPorts[0], tp.ClientPorts[1], localRTP, localRTCP)
	}

	// interleaved RTP/RTCP over TCP
	return fmt.Sprintf("RTP/AVP/TCP;interleaved=%d-%d", tp.Interleaved[0], tp.Interleaved[1])
}

// func to build RTP packets (header + no payload)
func buildRTPPacket() []byte {
	// RTP header: Version=2, P=0, X=0, CC=0, M=0, PT=96
	seq := uint16(rand.Uint32())
	ts := uint32(time.Now().UnixNano() / 1e6 * 90) // 90kHz clock
	ssrc := rand.Uint32()
	packet := make([]byte, 12)
	packet[0] = 0x80
	packet[1] = 96
	binary.BigEndian.PutUint16(packet[2:], seq)
	binary.BigEndian.PutUint32(packet[4:], ts)
	binary.BigEndian.PutUint32(packet[8:], ssrc)

	return packet
}

// func to wrap and send an RTP packet over the RTSP TCP connection
func sendInterleaved(conn net.Conn, packet []byte, channel byte) error {
	header := []byte{0x24, channel, byte(len(packet) >> 8), byte(len(packet) & 0xFF)}

	if _, err := conn.Write(header); err != nil {
		return err
	}

	_, err := conn.Write(packet)
	return err
}
