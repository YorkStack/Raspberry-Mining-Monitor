package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Webhook posts alerts as JSON to a single configured URL. It is generic enough
// for ntfy, a Slack/Discord incoming webhook wrapper, or any HTTP receiver.
//
// The URL comes only from the operator's config, never from observed data, and
// is never logged. Delivery is outbound only.
type Webhook struct {
	url  string
	http *http.Client
}

// NewWebhook creates a notifier for the given URL.
func NewWebhook(url string, timeout time.Duration) *Webhook {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Webhook{url: url, http: &http.Client{Timeout: timeout}}
}

// payload is the JSON body posted for each alert.
type payload struct {
	Miner   string  `json:"miner"`
	Kind    string  `json:"kind"`
	Level   string  `json:"level"`
	Message string  `json:"message"`
	Value   float64 `json:"value"`
	Source  string  `json:"source"`
}

// Notify delivers one alert.
func (w *Webhook) Notify(ctx context.Context, a Alert) error {
	body, err := json.Marshal(payload{
		Miner: a.Miner, Kind: string(a.Kind), Level: a.Level,
		Message: a.Message, Value: a.Value, Source: "raspberry-mining-monitor",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("alert webhook: status %d", resp.StatusCode)
	}
	return nil
}
