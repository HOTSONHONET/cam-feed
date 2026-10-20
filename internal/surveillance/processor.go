package surveillance

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cam-feed/internal/alerts"
	"cam-feed/internal/detector"
)

type Config struct {
	QueueSize      int
	MinInterval    time.Duration
	AlertCooldown  time.Duration
	MinScore       float32
	Labels         []string
	SnapshotDir    string
}

func DefaultConfig() Config {
	return Config{
		QueueSize:     8,
		MinInterval:   500 * time.Millisecond,
		AlertCooldown: 30 * time.Second,
		MinScore:      0.65,
		Labels:        []string{"person"},
		SnapshotDir:   "snapshots",
	}
}

type frameJob struct {
	deviceID string
	frame    []byte
	at       time.Time
}

type Processor struct {
	cfg      Config
	notifier alerts.Notifier
	queue    chan frameJob

	detMu sync.RWMutex
	det   detector.Detector

	stateMu   sync.Mutex
	lastInfer map[string]time.Time
	lastAlert map[string]time.Time
	labels    map[string]struct{}
}

func New(cfg Config, det detector.Detector, notifier alerts.Notifier) *Processor {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 8
	}
	if cfg.MinInterval <= 0 {
		cfg.MinInterval = 500 * time.Millisecond
	}
	if cfg.AlertCooldown <= 0 {
		cfg.AlertCooldown = 30 * time.Second
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.65
	}
	if cfg.SnapshotDir == "" {
		cfg.SnapshotDir = "snapshots"
	}

	labels := make(map[string]struct{}, len(cfg.Labels))
	for _, label := range cfg.Labels {
		label = strings.TrimSpace(strings.ToLower(label))
		if label != "" {
			labels[label] = struct{}{}
		}
	}

	return &Processor{
		cfg:       cfg,
		det:       det,
		notifier:  notifier,
		queue:     make(chan frameJob, cfg.QueueSize),
		lastInfer: make(map[string]time.Time),
		lastAlert: make(map[string]time.Time),
		labels:    labels,
	}
}

func (p *Processor) SetDetector(det detector.Detector) {
	p.detMu.Lock()
	p.det = det
	p.detMu.Unlock()
}

func (p *Processor) Start(ctx context.Context) {
	go p.worker(ctx)
}

func (p *Processor) Submit(deviceID string, frame []byte) {
	if p == nil || len(frame) == 0 {
		return
	}

	now := time.Now()

	p.stateMu.Lock()
	if last := p.lastInfer[deviceID]; !last.IsZero() && now.Sub(last) < p.cfg.MinInterval {
		p.stateMu.Unlock()
		return
	}
	p.lastInfer[deviceID] = now
	p.stateMu.Unlock()

	frameCopy := append([]byte(nil), frame...)
	job := frameJob{
		deviceID: deviceID,
		frame:    frameCopy,
		at:       now,
	}

	select {
	case p.queue <- job:
	default:
		// Surveillance should favor recent frames over building a backlog.
	}
}

func (p *Processor) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-p.queue:
			p.process(ctx, job)
		}
	}
}

func (p *Processor) process(ctx context.Context, job frameJob) {
	p.detMu.RLock()
	det := p.det
	if det == nil {
		p.detMu.RUnlock()
		return
	}
	detections, err := det.Detect(job.frame)
	p.detMu.RUnlock()
	if err != nil {
		log.Printf("[surveillance] detect failed for %s: %v", job.deviceID, err)
		return
	}

	matches := p.filterDetections(detections)
	if len(matches) == 0 || !p.canAlert(job.deviceID, job.at) {
		return
	}

	path, err := p.saveSnapshot(job)
	if err != nil {
		log.Printf("[surveillance] snapshot save failed for %s: %v", job.deviceID, err)
		return
	}

	p.markAlert(job.deviceID, job.at)

	if p.notifier != nil {
		event := alerts.Event{
			DeviceID:     job.deviceID,
			Detections:   matches,
			SnapshotPath: path,
			CapturedAt:   job.at,
		}
		if err := p.notifier.Notify(ctx, event); err != nil {
			log.Printf("[surveillance] alert failed for %s: %v", job.deviceID, err)
		}
	}
}

func (p *Processor) filterDetections(detections []detector.Detection) []detector.Detection {
	out := make([]detector.Detection, 0, len(detections))
	for _, d := range detections {
		if d.Score < p.cfg.MinScore {
			continue
		}

		if len(p.labels) > 0 {
			if _, ok := p.labels[strings.ToLower(d.Label)]; !ok {
				continue
			}
		}

		out = append(out, d)
	}
	return out
}

func (p *Processor) canAlert(deviceID string, now time.Time) bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	last := p.lastAlert[deviceID]
	return last.IsZero() || now.Sub(last) >= p.cfg.AlertCooldown
}

func (p *Processor) markAlert(deviceID string, now time.Time) {
	p.stateMu.Lock()
	p.lastAlert[deviceID] = now
	p.stateMu.Unlock()
}

func (p *Processor) saveSnapshot(job frameJob) (string, error) {
	if err := os.MkdirAll(p.cfg.SnapshotDir, 0o755); err != nil {
		return "", err
	}

	device := sanitize(job.deviceID)
	name := fmt.Sprintf("%s_%s.jpg", device, job.at.UTC().Format("20060102T150405.000Z"))
	path := filepath.Join(p.cfg.SnapshotDir, name)

	if err := os.WriteFile(path, job.frame, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "camera"
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
