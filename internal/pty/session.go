package pty

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Damnatti/poopilot/internal/protocol"
	"github.com/google/uuid"

	gopty "github.com/creack/pty"
)

// Session represents a single PTY session wrapping a CLI process.
type Session struct {
	ID      string
	Command string
	Args    []string
	Status  string // "running", "exited"
	Created int64

	ptmx       *os.File
	cmd        *exec.Cmd
	output     *RingBuffer
	mu         sync.Mutex
	onOutputs  []func([]byte)
	onExit     func(exitCode int)
	done       chan struct{}
}

// NewSession creates a new PTY session with a unique ID.
func NewSession(command string, args []string) *Session {
	id := uuid.New().String()
	// Use first 16 hex chars (no dashes) as session ID for protocol compatibility.
	shortID := id[:8] + id[9:13] + id[14:18]

	return &Session{
		ID:      shortID,
		Command: command,
		Args:    args,
		Status:  "created",
		output:  NewRingBuffer(65536), // 64KB scrollback
		done:    make(chan struct{}),
	}
}

// Start spawns the process in a PTY and begins reading output.
func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cmd = exec.Command(s.Command, s.Args...)
	s.cmd.Env = os.Environ()

	ptmx, err := gopty.Start(s.cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	s.ptmx = ptmx
	s.Status = "running"

	go s.readLoop()
	go s.waitLoop()

	return nil
}

// Write sends data to the PTY stdin.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.Lock()
	ptmx := s.ptmx
	s.mu.Unlock()

	if ptmx == nil {
		return 0, fmt.Errorf("session not started")
	}
	return ptmx.Write(data)
}

// Resize changes the PTY window size.
func (s *Session) Resize(rows, cols uint16) error {
	s.mu.Lock()
	ptmx := s.ptmx
	s.mu.Unlock()

	if ptmx == nil {
		return fmt.Errorf("session not started")
	}

	ws := struct {
		Row uint16
		Col uint16
		X   uint16
		Y   uint16
	}{Row: rows, Col: cols}

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		ptmx.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// ScrollbackSnapshot returns the contents of the ring buffer.
func (s *Session) ScrollbackSnapshot() []byte {
	return s.output.Bytes()
}

// OnOutput adds a callback for new output data. Multiple callbacks are supported.
func (s *Session) OnOutput(f func([]byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onOutputs = append(s.onOutputs, f)
}

// OnExit sets the callback for process exit.
func (s *Session) OnExit(f func(int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExit = f
}

// Close kills the process and cleans up.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	if s.ptmx != nil {
		s.ptmx.Close()
	}
	return nil
}

// Done returns a channel that is closed when the session process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Info returns a protocol-compatible session info struct.
func (s *Session) Info() protocol.SessionInfo {
	s.mu.Lock()
	status := s.Status
	s.mu.Unlock()
	return protocol.SessionInfo{
		ID:      s.ID,
		Command: s.Command,
		Status:  status,
		Created: s.Created,
	}
}

func (s *Session) readLoop() {
	buf := make([]byte, 16384)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			s.output.Write(data)

			s.mu.Lock()
			cbs := make([]func([]byte), len(s.onOutputs))
			copy(cbs, s.onOutputs)
			s.mu.Unlock()
			for _, cb := range cbs {
				cb(data)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) waitLoop() {
	exitCode := 0
	if err := s.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	s.mu.Lock()
	s.Status = "exited"
	cb := s.onExit
	s.mu.Unlock()

	if cb != nil {
		cb(exitCode)
	}
	close(s.done)
}

// RingBuffer is a circular byte buffer for scrollback.
type RingBuffer struct {
	buf  []byte
	size int
	pos  int
	full bool
	mu   sync.Mutex
}

// NewRingBuffer creates a ring buffer of the given size.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Write appends data to the ring buffer, overwriting oldest data if full.
func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n >= r.size {
		// Data larger than buffer — keep only the last `size` bytes
		copy(r.buf, p[n-r.size:])
		r.pos = 0
		r.full = true
		return n, nil
	}

	// How much fits before wrap
	first := r.size - r.pos
	if first >= n {
		copy(r.buf[r.pos:], p)
	} else {
		copy(r.buf[r.pos:], p[:first])
		copy(r.buf, p[first:])
	}

	r.pos = (r.pos + n) % r.size
	if !r.full && r.pos < n {
		// We wrapped — but only set full if we actually wrote past the start.
		// More precisely: full if total written >= size.
	}
	// Simpler: track total written
	if !r.full {
		// Check if we wrapped
		oldPos := (r.pos - n + r.size) % r.size
		if r.pos <= oldPos && n > 0 {
			r.full = true
		}
	} else {
		r.full = true
	}

	return n, nil
}

// Bytes returns the buffer contents in order (oldest first).
func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		result := make([]byte, r.pos)
		copy(result, r.buf[:r.pos])
		return result
	}

	result := make([]byte, r.size)
	// Data from pos to end is oldest
	first := r.size - r.pos
	copy(result, r.buf[r.pos:])
	copy(result[first:], r.buf[:r.pos])
	return result
}

// Len returns the number of bytes currently in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.full {
		return r.size
	}
	return r.pos
}
