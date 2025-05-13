package rtsp

import (
	"bufio"
	"fmt"
	"log"
	"net"
)

func HandleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		// Parsing incoming RTSP request
		req, err := readRequest(reader)
		if err != nil {
			log.Printf("[ERROR] error reading request: %v", err)
			return
		}

		// Prepare response headers, echoing CSeq per RTSP spec
		respHeaders := map[string]string{
			"CSeq": req.Headers["Cseq"],
		}

		switch req.Method {
		case "OPTIONS":
			// Listing supported methods
			respHeaders["Public"] = "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN"
			sendResponse(conn, 200, respHeaders, nil)

		case "DESCRIBE":
			// Return session description (SDP)
			sdp := generateSDP(req.URI)
			respHeaders["Content-Base"] = req.URI + "/"
			respHeaders["Content-Type"] = "application/sdp"
			respHeaders["Content-Length"] = fmt.Sprint(len(sdp))
			sendResponse(conn, 200, respHeaders, []byte(sdp))

		case "SETUP":
			// Allocate transport (UDP or TCP)
			sessionID := setupSession(req, conn)
			respHeaders["Session"] = sessionID
			respHeaders["Transport"] = transportResponse(sessions[sessionID])
			sendResponse(conn, 200, respHeaders, nil)

		case "PLAY":
			// Start RTP streaming for this session
			startRTPStreaming(req.Headers["Session"])
			sendResponse(conn, 200, respHeaders, nil)

		case "TEARDOWN":
			// Clean up session resources
			teardownSession(req.Headers["Session"])
			sendResponse(conn, 200, respHeaders, nil)

		default:
			// Method not allowed
			sendResponse(conn, 405, respHeaders, nil)
		}
	}
}
