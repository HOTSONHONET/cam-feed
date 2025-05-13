package rtsp

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type RTSPRequest struct {
	Method  string
	URI     string
	Headers map[string]string
	Body    []byte
}

// func to parse the incoming RTSP request
func readRequest(r *bufio.Reader) (*RTSPRequest, error) {
	// Read request line: METHOD URI RTSP/1.0
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}

	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 3 {
		return nil, fmt.Errorf("malformed request line: %s", line)
	}

	req := &RTSPRequest{
		Method:  parts[0],
		URI:     parts[1],
		Headers: make(map[string]string),
	}

	// Read headers until empty line
	for {
		headerLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}

		headerLine = strings.TrimSpace(headerLine)
		if headerLine == "" {
			break
		}

		kv := strings.SplitN(headerLine, ":", 2)
		req.Headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}

	// Reading body if Content-Length is set
	if contentLen, ok := req.Headers["Content-Length"]; ok {
		var len int
		fmt.Sscanf(contentLen, "%d", &len)
		buf := make([]byte, len)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}

		req.Body = buf
	}

	return req, nil
}

func sendResponse(w io.Writer, statusCode int, headers map[string]string, body []byte) {
	fmt.Fprintf(w, "RTSP/1.0 %d %s\r\n", statusCode, statusText(statusCode))
	for k, v := range headers {
		fmt.Fprintf(w, "%s: %s\r\n", k, v)
	}

	fmt.Fprintf(w, "\r\n")
	if len(body) > 0 {
		w.Write(body)
	}
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	default:
		return ""
	}
}
