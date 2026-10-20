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
	if webhook := strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL")); webhook != "" {
		notifier = alerts.NewSlackNotifier(webhook)
		log.Printf("[alerts] Slack notifications enabled")
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
