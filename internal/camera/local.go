package camera

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"

	"cam-feed/internal/hub"
)

type LocalConfig struct {
	Enabled     bool
	Device      string
	DeviceID    string
	Room        string
	Width       int
	Height      int
	InputFPS    int
	OutputFPS   int
	JPEGQuality int
}

func DefaultLocalConfig() LocalConfig {
	return LocalConfig{
		Device:      "/dev/video0",
		DeviceID:    "host-camera",
		Room:        hub.DefaultRoom,
		Width:       1280,
		Height:      720,
		InputFPS:    10,
		OutputFPS:   2,
		JPEGQuality: 5,
	}
}

func RunLocal(ctx context.Context, cfg LocalConfig, h *hub.Hub) error {
	if !cfg.Enabled {
		return nil
	}
	if h == nil {
		return fmt.Errorf("camera: hub is required")
	}
	if cfg.Device == "" {
		return fmt.Errorf("camera: device is required")
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = "host-camera"
	}
	if cfg.Room == "" {
		cfg.Room = hub.DefaultRoom
	}
	if cfg.Width <= 0 { cfg.Width = 1280 }
	if cfg.Height <= 0 { cfg.Height = 720 }
	if cfg.InputFPS <= 0 { cfg.InputFPS = 10 }
	if cfg.OutputFPS <= 0 { cfg.OutputFPS = 2 }
	if cfg.JPEGQuality <= 0 { cfg.JPEGQuality = 5 }

	meta := hub.StreamMeta{
		DeviceID: cfg.DeviceID,
		Room: cfg.Room,
		Width: cfg.Width,
		Height: cfg.Height,
		FPS: cfg.OutputFPS,
	}
	h.RegisterLocalStream(meta)
	defer h.UnregisterLocalStream(cfg.DeviceID, cfg.Room)

	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-f", "v4l2",
		"-framerate", strconv.Itoa(cfg.InputFPS),
		"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-i", cfg.Device,
		"-vf", fmt.Sprintf("fps=%d", cfg.OutputFPS),
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-q:v", strconv.Itoa(cfg.JPEGQuality),
		"pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil { return fmt.Errorf("camera: stdout pipe: %w", err) }
	stderr, err := cmd.StderrPipe()
	if err != nil { return fmt.Errorf("camera: stderr pipe: %w", err) }
	if err := cmd.Start(); err != nil { return fmt.Errorf("camera: start ffmpeg: %w", err) }

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[camera] ffmpeg: %s", scanner.Text())
		}
	}()

	log.Printf("[camera] local stream started device=%s id=%s room=%s %dx%d input_fps=%d output_fps=%d",
		cfg.Device, cfg.DeviceID, cfg.Room, cfg.Width, cfg.Height, cfg.InputFPS, cfg.OutputFPS)

	reader := bufio.NewReaderSize(stdout, 1024*1024)
	for {
		frame, err := readJPEG(reader)
		if err != nil {
			if ctx.Err() != nil {
				_ = cmd.Wait()
				return nil
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("camera: read JPEG: %w", err)
		}
		h.PublishLocalFrame(meta, frame)
	}
}

func readJPEG(r *bufio.Reader) ([]byte, error) {
	var frame bytes.Buffer
	foundSOI := false
	var prev byte

	for {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && frame.Len() > 0 { return nil, io.ErrUnexpectedEOF }
			return nil, err
		}

		if !foundSOI {
			if prev == 0xff && b == 0xd8 {
				foundSOI = true
				frame.WriteByte(0xff)
				frame.WriteByte(0xd8)
			}
			prev = b
			continue
		}

		frame.WriteByte(b)
		if prev == 0xff && b == 0xd9 {
			return append([]byte(nil), frame.Bytes()...), nil
		}
		prev = b
	}
}
