package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SlackNotifier struct {
	webhookURL string
	botToken   string
	channelID  string
	client     *http.Client
}

func NewSlackNotifier(webhookURL, botToken, channelID string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: strings.TrimSpace(webhookURL),
		botToken:   strings.TrimSpace(botToken),
		channelID:  strings.TrimSpace(channelID),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *SlackNotifier) Notify(ctx context.Context, event Event) error {
	if s == nil {
		return nil
	}

	labels := make([]string, 0, len(event.Detections))
	for _, d := range event.Detections {
		labels = append(labels, fmt.Sprintf("%s %.0f%%", d.Label, d.Score*100))
	}

	text := fmt.Sprintf(
		"⚠️ cam-feed detection\nDevice: %s\nDetected: %s\nTime: %s",
		event.DeviceID,
		strings.Join(labels, ", "),
		event.CapturedAt.Format(time.RFC3339),
	)

	if s.botToken != "" && s.channelID != "" && event.SnapshotPath != "" {
		if err := s.uploadSnapshot(ctx, event.SnapshotPath, text); err == nil {
			return nil
		} else if s.webhookURL == "" {
			return err
		}
	}

	if s.webhookURL == "" {
		return nil
	}

	return s.postWebhook(ctx, text+"\nSnapshot: "+event.SnapshotPath)
}

func (s *SlackNotifier) postWebhook(ctx context.Context, text string) error {
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

type slackUploadURLResponse struct {
	OK        bool   `json:"ok"`
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
	Error     string `json:"error"`
}

type slackAPIResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (s *SlackNotifier) uploadSnapshot(ctx context.Context, snapshotPath, comment string) error {
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("slack: read snapshot: %w", err)
	}
	filename := filepath.Base(snapshotPath)

	uploadURL, fileID, err := s.getUploadURL(ctx, filename, len(data))
	if err != nil {
		return err
	}
	if err := s.uploadBytes(ctx, uploadURL, filename, data); err != nil {
		return err
	}
	return s.completeUpload(ctx, fileID, filename, comment)
}

func (s *SlackNotifier) getUploadURL(ctx context.Context, filename string, length int) (string, string, error) {
	form := url.Values{}
	form.Set("filename", filename)
	form.Set("length", fmt.Sprintf("%d", length))

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://slack.com/api/files.getUploadURLExternal",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.botToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var out slackUploadURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if !out.OK {
		return "", "", fmt.Errorf("slack files.getUploadURLExternal: %s", out.Error)
	}
	return out.UploadURL, out.FileID, nil
}

func (s *SlackNotifier) uploadBytes(ctx context.Context, uploadURL, filename string, data []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack file upload returned %s", resp.Status)
	}
	return nil
}

func (s *SlackNotifier) completeUpload(ctx context.Context, fileID, title, comment string) error {
	filesJSON, err := json.Marshal([]map[string]string{
		{"id": fileID, "title": title},
	})
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("files", string(filesJSON))
	form.Set("channel_id", s.channelID)
	form.Set("initial_comment", comment)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://slack.com/api/files.completeUploadExternal",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.botToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out slackAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("slack files.completeUploadExternal: %s", out.Error)
	}
	return nil
}
