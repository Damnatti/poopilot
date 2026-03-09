package qr

import (
	"strings"
	"testing"
)

func TestRenderToTerminal_ReturnsANSI(t *testing.T) {
	qr, err := RenderToTerminal("https://example.com")
	if err != nil {
		t.Fatalf("RenderToTerminal failed: %v", err)
	}

	if qr == "" {
		t.Fatal("QR string should not be empty")
	}

	// Should contain block characters (Unicode half-blocks)
	if !strings.ContainsAny(qr, "█▀▄ ") {
		t.Errorf("QR should contain block characters, got: %q", qr[:100])
	}
}

func TestRenderToTerminal_DifferentInputs(t *testing.T) {
	qr1, _ := RenderToTerminal("hello")
	qr2, _ := RenderToTerminal("world")

	if qr1 == qr2 {
		t.Error("different inputs should produce different QR codes")
	}
}

func TestGeneratePairingQR_FormatsURL(t *testing.T) {
	qr, err := GeneratePairingQR("192.168.1.100", 3131, "compressed-offer-data")
	if err != nil {
		t.Fatalf("GeneratePairingQR failed: %v", err)
	}

	if qr == "" {
		t.Fatal("QR should not be empty")
	}
}

func TestRenderToTerminal_LongContent(t *testing.T) {
	// 500 chars — should still work (within QR capacity)
	content := strings.Repeat("a", 500)
	qr, err := RenderToTerminal(content)
	if err != nil {
		t.Fatalf("RenderToTerminal failed for long content: %v", err)
	}

	if qr == "" {
		t.Fatal("QR should not be empty")
	}
}
