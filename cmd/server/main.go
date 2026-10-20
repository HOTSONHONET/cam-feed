package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cam-feed/internal/alerts"
	"cam-feed/internal/camera"
	"cam-feed/internal/detector"
	"cam-feed/internal/hub"
	"cam-feed/internal/surveillance"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	detector.ConfigureOrtRuntime(
		env("ORT_CPU_LIB", "third_party/onnxruntime/1.24.2/onnxruntime_cpu.so"),
		env("ORT_GPU_LIB", "third_party/onnxruntime/1.24.2/onnxruntime_gpu.so"),
		env("ORT_GPU_LIB_DIR", "third_party/onnxruntime/1.24.2/gpu_libs"),
	)

	cfg := detector.DefaultConfig()
	cfg.Device = detector.Device(strings.ToLower(env("INFERENCE_DEVICE", "gpu")))
	cfg.ScoreThreshold = float32(envFloat("MODEL_SCORE_THRESHOLD", float64(cfg.ScoreThreshold)))

	det, activeDevice, warn, err := detector.NewWithInfo(cfg)
	if err != nil {
		log.Fatalf("[detector] init failed: %v", err)
	}
	defer det.Close()

	if warn != nil {
		log.Printf("[detector] %v", warn)
	}
	log.Printf("[detector] active device: %s", activeDevice)

	var notifier alerts.Notifier
	webhook := strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL"))
	botToken := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN"))
	channelID := strings.TrimSpace(os.Getenv("SLACK_CHANNEL_ID"))
	if webhook != "" || (botToken != "" && channelID != "") {
		notifier = alerts.NewSlackNotifier(webhook, botToken, channelID)
		if botToken != "" && channelID != "" {
			log.Printf("[alerts] Slack snapshot uploads enabled")
		} else {
			log.Printf("[alerts] Slack text notifications enabled")
		}
	}

	survCfg := surveillance.DefaultConfig()
	survCfg.MinInterval = envDuration("DETECTION_INTERVAL", survCfg.MinInterval)
	survCfg.AlertCooldown = envDuration("ALERT_COOLDOWN", survCfg.AlertCooldown)
	survCfg.MinScore = float32(envFloat("ALERT_MIN_SCORE", float64(survCfg.MinScore)))
	survCfg.SnapshotDir = env("SNAPSHOT_DIR", survCfg.SnapshotDir)
	survCfg.Labels = envList("ALERT_LABELS", survCfg.Labels)

	processor := surveillance.New(survCfg, det, notifier)
	processor.Start(ctx)

	h := hub.New()
	h.SetFrameProcessor(processor)

	localCfg := camera.DefaultLocalConfig()
	localCfg.Enabled = envBool("LOCAL_CAMERA_ENABLED", false)
	localCfg.Device = env("LOCAL_CAMERA_DEVICE", localCfg.Device)
	localCfg.DeviceID = env("LOCAL_CAMERA_DEVICE_ID", localCfg.DeviceID)
	localCfg.Room = env("LOCAL_CAMERA_ROOM", localCfg.Room)
	localCfg.Width = envInt("LOCAL_CAMERA_WIDTH", localCfg.Width)
	localCfg.Height = envInt("LOCAL_CAMERA_HEIGHT", localCfg.Height)
	localCfg.InputFPS = envInt("LOCAL_CAMERA_INPUT_FPS", localCfg.InputFPS)
	localCfg.OutputFPS = envInt("LOCAL_CAMERA_OUTPUT_FPS", localCfg.OutputFPS)
	localCfg.JPEGQuality = envInt("LOCAL_CAMERA_JPEG_QUALITY", localCfg.JPEGQuality)

	if localCfg.Enabled {
		go func() {
			if err := camera.RunLocal(ctx, localCfg, h); err != nil && ctx.Err() == nil {
				log.Printf("[camera] local camera stopped: %v", err)
			}
		}()
	}

	log.Printf("[server] starting cam-feed headless hub")
	if err := h.StartServers(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("[server] %v", err)
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		log.Printf("[config] invalid %s=%q, using %v", key, value, fallback)
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("[config] invalid %s=%q, using %s", key, value, fallback)
		return fallback
	}
	return parsed
}

func envList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("[config] invalid %s=%q, using %t", key, value, fallback)
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("[config] invalid %s=%q, using %d", key, value, fallback)
		return fallback
	}
	return parsed
}
