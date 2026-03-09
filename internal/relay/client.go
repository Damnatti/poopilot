package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PostOffer uploads the compressed offer SDP to the relay.
func PostOffer(relayURL, roomID, offer string) error {
	url := fmt.Sprintf("%s/relay/%s/offer", strings.TrimRight(relayURL, "/"), roomID)
	req, err := http.NewRequest("PUT", url, strings.NewReader(offer))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("relay post offer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("relay post offer: %s %s", resp.Status, body)
	}
	return nil
}

// PollAnswer polls the relay for the answer SDP until found or context is cancelled.
func PollAnswer(ctx context.Context, relayURL, roomID string) (string, error) {
	url := fmt.Sprintf("%s/relay/%s/answer", strings.TrimRight(relayURL, "/"), roomID)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			answer, err := fetchAnswer(url)
			if err != nil {
				continue // retry on error
			}
			if answer != "" {
				return answer, nil
			}
		}
	}
}

func fetchAnswer(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil // not ready yet
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay: %s", resp.Status)
	}

	var result struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Answer, nil
}
