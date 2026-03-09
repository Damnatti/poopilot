package approval

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
)

// ansiEscape matches ANSI escape sequences.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// Pattern is a compiled regex pattern for detecting approval prompts.
type Pattern struct {
	Name  string
	Regex *regexp.Regexp
	Tool  string // "claude", "codex", "generic"
}

// Detection represents a detected approval prompt.
type Detection struct {
	PatternName string
	Tool        string // which tool matched ("claude", "codex", "generic")
	Prompt      string // the matched text
	Context     string // surrounding lines
	LineOffset  int    // line number in the scanned text
}

// Detector scans terminal output for yes/no approval prompts.
type Detector struct {
	patterns []*Pattern
	seen     map[[32]byte]bool // deduplication by content hash
	mu       sync.Mutex
}

// NewDetector creates a detector with default patterns for common AI CLI tools.
func NewDetector() *Detector {
	d := &Detector{
		seen: make(map[[32]byte]bool),
	}

	// Claude Code patterns
	d.addPattern("claude_proceed", `(?i)(do you want to proceed|proceed\s*\?\s*\[)`, "claude")
	d.addPattern("claude_allow", `(?i)(allow once|allow always|allow this|deny this)`, "claude")
	d.addPattern("claude_yesno", `(?i)\[?(Y/n|y/N|Yes/No)\]?`, "claude")
	d.addPattern("claude_tool", `(?i)(Allow|Deny|approve)\s+(tool|this action|this command)`, "claude")
	d.addPattern("claude_permission", `(?i)(do you want to allow|would you like to allow|permission to)`, "claude")

	// Codex patterns
	d.addPattern("codex_approve", `(?i)(approve|reject|confirm)\s*(this|the)?\s*(change|action|edit)`, "codex")

	// Generic patterns
	d.addPattern("generic_yesno", `\?\s*\[?(Y/n|y/N)\]?\s*$`, "generic")
	d.addPattern("generic_proceed", `(?i)(proceed|continue|accept|confirm)\s*\?\s*$`, "generic")
	d.addPattern("generic_yn", `(?i)\(y/n\)\s*:?\s*$`, "generic")

	return d
}

// AddPattern adds a custom pattern to the detector.
func (d *Detector) AddPattern(name, regex, tool string) error {
	return d.addPattern(name, regex, tool)
}

func (d *Detector) addPattern(name, regex, tool string) error {
	r, err := regexp.Compile(regex)
	if err != nil {
		return err
	}
	d.patterns = append(d.patterns, &Pattern{
		Name:  name,
		Regex: r,
		Tool:  tool,
	})
	return nil
}

// Scan checks the given output for approval prompts.
// Returns all new detections (not previously seen).
func (d *Detector) Scan(output []byte) []Detection {
	d.mu.Lock()
	defer d.mu.Unlock()

	text := string(output)
	lines := strings.Split(text, "\n")
	var detections []Detection

	for lineIdx, line := range lines {
		// Strip ANSI escape codes for matching
		clean := ansiEscape.ReplaceAllString(line, "")
		clean = strings.TrimSpace(clean)

		if clean == "" {
			continue
		}

		// Deduplicate by content hash
		hash := sha256.Sum256([]byte(clean))
		if d.seen[hash] {
			continue
		}

		for _, p := range d.patterns {
			if loc := p.Regex.FindStringIndex(clean); loc != nil {
				d.seen[hash] = true

				context := getContext(lines, lineIdx, 2)
				detections = append(detections, Detection{
					PatternName: p.Name,
					Tool:        p.Tool,
					Prompt:      clean[loc[0]:loc[1]],
					Context:     ansiEscape.ReplaceAllString(context, ""),
					LineOffset:  lineIdx,
				})
				break // one detection per line
			}
		}
	}

	return detections
}

// Reset clears the deduplication state.
func (d *Detector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[[32]byte]bool)
}

// getContext returns surrounding lines around the given index.
func getContext(lines []string, idx, radius int) string {
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + radius + 1
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}
