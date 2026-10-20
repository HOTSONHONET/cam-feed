package alerts

import (
	"context"
	"time"

	"cam-feed/internal/detector"
)

type Event struct {
	DeviceID     string
	Detections   []detector.Detection
	SnapshotPath string
	CapturedAt   time.Time
}

type Notifier interface {
	Notify(ctx context.Context, event Event) error
}
