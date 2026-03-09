package approval

import (
	"testing"
)

func TestDetector_Claude_YesNo(t *testing.T) {
	d := NewDetector()
	output := []byte("Some output\nDo you want to proceed? [Y/n]\nMore text")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected at least one detection")
	}

	found := false
	for _, det := range detections {
		if det.PatternName == "claude_yesno" || det.PatternName == "claude_proceed" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected claude pattern match, got: %+v", detections)
	}
}

func TestDetector_Claude_AllowOnce(t *testing.T) {
	d := NewDetector()
	output := []byte("Claude wants to run: rm -rf /tmp/test\nAllow once  |  Allow always  |  Deny this")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection for 'Allow once'")
	}

	if detections[0].PatternName != "claude_allow" {
		t.Errorf("expected claude_allow, got %q", detections[0].PatternName)
	}
}

func TestDetector_Claude_ToolUse(t *testing.T) {
	d := NewDetector()
	output := []byte("Allow this action? The tool wants to edit files.")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection")
	}
}

func TestDetector_Codex_Approve(t *testing.T) {
	d := NewDetector()
	output := []byte("Review the changes:\n+ added line\n- removed line\nApprove this change?")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection for codex approve")
	}

	if detections[0].Tool != "codex" {
		t.Errorf("expected tool 'codex', got %q", detections[0].Tool)
	}
}

func TestDetector_Generic_YesNo(t *testing.T) {
	d := NewDetector()
	output := []byte("Install packages? [Y/n]")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection for generic [Y/n]")
	}
}

func TestDetector_Generic_Proceed(t *testing.T) {
	d := NewDetector()
	output := []byte("Files will be overwritten.\nProceed?")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection for 'Proceed?'")
	}
}

func TestDetector_Generic_YN_Parens(t *testing.T) {
	d := NewDetector()
	output := []byte("Are you sure? (y/n):")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection for '(y/n):'")
	}
}

func TestDetector_NoMatch(t *testing.T) {
	d := NewDetector()
	output := []byte("Compiling project...\nBuild successful.\n42 tests passed.")

	detections := d.Scan(output)
	if len(detections) != 0 {
		t.Errorf("expected no detections, got %d: %+v", len(detections), detections)
	}
}

func TestDetector_Deduplication(t *testing.T) {
	d := NewDetector()
	output := []byte("Proceed? [Y/n]")

	det1 := d.Scan(output)
	det2 := d.Scan(output)

	if len(det1) == 0 {
		t.Fatal("first scan should detect")
	}
	if len(det2) != 0 {
		t.Errorf("second scan should return empty (deduplicated), got %d", len(det2))
	}
}

func TestDetector_Reset_ClearsDedup(t *testing.T) {
	d := NewDetector()
	output := []byte("Proceed? [Y/n]")

	d.Scan(output)
	d.Reset()
	det := d.Scan(output)

	if len(det) == 0 {
		t.Error("after Reset, should detect again")
	}
}

func TestDetector_Context_SurroundingLines(t *testing.T) {
	d := NewDetector()
	output := []byte("line 0\nline 1\nProceed? [Y/n]\nline 3\nline 4")

	detections := d.Scan(output)
	if len(detections) == 0 {
		t.Fatal("expected detection")
	}

	ctx := detections[0].Context
	// Context should include lines 0-4 (radius 2 around line 2)
	if ctx == "" {
		t.Error("context should not be empty")
	}
	// Should contain the surrounding lines
	if len(ctx) < 10 {
		t.Errorf("context seems too short: %q", ctx)
	}
}

func TestDetector_CustomPattern(t *testing.T) {
	d := NewDetector()
	err := d.AddPattern("custom", `DEPLOY NOW\?`, "custom")
	if err != nil {
		t.Fatalf("AddPattern failed: %v", err)
	}

	output := []byte("Ready to ship.\nDEPLOY NOW?")
	detections := d.Scan(output)

	if len(detections) == 0 {
		t.Fatal("expected custom pattern detection")
	}
	if detections[0].PatternName != "custom" {
		t.Errorf("expected 'custom', got %q", detections[0].PatternName)
	}
}

func TestDetector_InvalidRegex(t *testing.T) {
	d := NewDetector()
	err := d.AddPattern("bad", `[invalid`, "test")
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestDetector_MultipleDetectionsInOneOutput(t *testing.T) {
	d := NewDetector()
	output := []byte("Allow this action?\nAlso proceed? [Y/n]")

	detections := d.Scan(output)
	if len(detections) < 2 {
		t.Errorf("expected 2 detections, got %d", len(detections))
	}
}
