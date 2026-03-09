package webrtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"time"
)

// TURNConfig holds TURN server configuration.
type TURNConfig struct {
	Host   string // e.g. "76.13.9.204"
	Secret string // shared secret for ephemeral credentials
}

// GenerateTURNCredentials creates time-limited TURN credentials using HMAC-SHA1.
// Compatible with coturn's use-auth-secret mode.
func GenerateTURNCredentials(cfg TURNConfig) (urls []string, username, credential string) {
	// Credentials valid for 24 hours
	expiry := time.Now().Add(24 * time.Hour).Unix()
	username = fmt.Sprintf("%d:poopilot", expiry)

	mac := hmac.New(sha1.New, []byte(cfg.Secret))
	mac.Write([]byte(username))
	credential = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	urls = []string{
		fmt.Sprintf("turn:%s:3478?transport=udp", cfg.Host),
		fmt.Sprintf("turn:%s:3478?transport=tcp", cfg.Host),
	}

	return urls, username, credential
}
