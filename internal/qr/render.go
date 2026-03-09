package qr

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// RenderToTerminal generates a QR code as a compact terminal string
// using Unicode half-block characters.
func RenderToTerminal(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("qr generate: %w", err)
	}

	return q.ToSmallString(false), nil
}

// GeneratePairingQR creates a QR code for the pairing URL.
// The URL format is: http://<host>:<port>/pair#<compressed-offer>
func GeneratePairingQR(host string, port int, compressedOffer string) (string, error) {
	url := fmt.Sprintf("http://%s:%d/pair#%s", host, port, compressedOffer)
	return RenderToTerminal(url)
}
