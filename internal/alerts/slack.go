package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: strings.TrimSpace(webhookURL),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *SlackNotifier) Notify(ctx context.Context, event Event) error {
	if s == nil || s.webhookURL == "" {
		return nil
	}

	labels := make([]string, 0, len(event.Detections))
	for _, d := range event.Detections {
		labels = append(labels, fmt.Sprintf("%s %.0f%%", d.Label, d.Score*100))
	}

	text := fmt.Sprintf(
		"⚠️ cam-feed detection\nDevice: %s\nDetected: %s\nSnapshot: %s\nTime: %s",
		event.DeviceID,
		strings.Join(labels, ", "),
		event.SnapshotPath,
		event.CapturedAt.Format(time.RFC3339),
	)

	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s", resp.Status)
	}

	return nil
}
