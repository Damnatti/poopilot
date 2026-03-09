package pty

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewSession_HasUniqueID(t *testing.T) {
	s1 := NewSession("echo", []string{"hello"})
	s2 := NewSession("echo", []string{"world"})

	if s1.ID == s2.ID {
		t.Errorf("sessions should have unique IDs, both got %q", s1.ID)
	}
	if len(s1.ID) != 16 {
		t.Errorf("session ID should be 16 chars, got %d: %q", len(s1.ID), s1.ID)
	}
}

func TestSession_Start_SpawnsProcess(t *testing.T) {
	s := NewSession("echo", []string{"hello"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	select {
	case <-s.Done():
		// process exited — good
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit in time")
	}

	if s.Status != "exited" {
		t.Errorf("status: got %q, want %q", s.Status, "exited")
	}
}

func TestSession_OnOutput_ReceivesStdout(t *testing.T) {
	s := NewSession("echo", []string{"hello world"})

	var buf []byte
	var mu sync.Mutex
	s.OnOutput(func(data []byte) {
		mu.Lock()
		buf = append(buf, data...)
		mu.Unlock()
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	output := string(buf)
	mu.Unlock()

	if !strings.Contains(output, "hello world") {
		t.Errorf("output should contain 'hello world', got %q", output)
	}
}

func TestSession_Write_SendsToStdin(t *testing.T) {
	// Use cat — it echoes stdin to stdout
	s := NewSession("cat", nil)

	var buf []byte
	var mu sync.Mutex
	s.OnOutput(func(data []byte) {
		mu.Lock()
		buf = append(buf, data...)
		mu.Unlock()
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	// Write to stdin
	_, err := s.Write([]byte("test input\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Wait for output
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	output := string(buf)
	mu.Unlock()

	if !strings.Contains(output, "test input") {
		t.Errorf("output should contain 'test input', got %q", output)
	}
}

func TestSession_Close_KillsProcess(t *testing.T) {
	s := NewSession("sleep", []string{"60"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	err := s.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case <-s.Done():
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit after Close")
	}
}

func TestSession_ScrollbackSnapshot(t *testing.T) {
	s := NewSession("echo", []string{"scrollback test"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	// Small delay for output to be captured
	time.Sleep(100 * time.Millisecond)

	snap := s.ScrollbackSnapshot()
	if !strings.Contains(string(snap), "scrollback test") {
		t.Errorf("scrollback should contain 'scrollback test', got %q", string(snap))
	}
}

func TestSession_OnExit_Called(t *testing.T) {
	s := NewSession("true", nil) // exits with code 0

	exitCh := make(chan int, 1)
	s.OnExit(func(code int) {
		exitCh <- code
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Errorf("exit code: got %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnExit not called")
	}
}

func TestSession_OnExit_NonZero(t *testing.T) {
	s := NewSession("false", nil) // exits with code 1

	exitCh := make(chan int, 1)
	s.OnExit(func(code int) {
		exitCh <- code
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Close()

	select {
	case code := <-exitCh:
		if code != 1 {
			t.Errorf("exit code: got %d, want 1", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnExit not called")
	}
}

func TestSession_Info(t *testing.T) {
	s := NewSession("echo", []string{"hi"})
	info := s.Info()

	if info.ID != s.ID {
		t.Errorf("ID mismatch")
	}
	if info.Command != "echo" {
		t.Errorf("Command: got %q, want %q", info.Command, "echo")
	}
	if info.Status != "created" {
		t.Errorf("Status: got %q, want %q", info.Status, "created")
	}
}

// --- RingBuffer tests ---

func TestRingBuffer_Write_Read(t *testing.T) {
	r := NewRingBuffer(10)
	r.Write([]byte("hello"))

	got := string(r.Bytes())
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRingBuffer_Wraps(t *testing.T) {
	r := NewRingBuffer(10)
	r.Write([]byte("1234567890")) // fills exactly
	r.Write([]byte("AB"))         // overwrites first 2

	got := string(r.Bytes())
	// Should be "34567890AB" — oldest data first
	if got != "34567890AB" {
		t.Errorf("got %q, want %q", got, "34567890AB")
	}
}

func TestRingBuffer_LargerThanBuffer(t *testing.T) {
	r := NewRingBuffer(5)
	r.Write([]byte("1234567890")) // 10 bytes into 5-byte buffer

	got := string(r.Bytes())
	if got != "67890" {
		t.Errorf("got %q, want %q", got, "67890")
	}
}

func TestRingBuffer_MultipleWrites(t *testing.T) {
	r := NewRingBuffer(8)
	r.Write([]byte("AAAA"))
	r.Write([]byte("BBBB"))
	r.Write([]byte("CC"))

	got := string(r.Bytes())
	// Buffer: AABBBBCC -> after wrap: BBBBCC? No:
	// Write AAAA: buf=[AAAA____], pos=4, full=false
	// Write BBBB: buf=[AAAABBBB], pos=0, full=true
	// Write CC:   buf=[CCAABBBB]? No: pos=0, write CC -> buf=[CC__BBBB]?
	// Actually: pos=0, write CC at pos 0: buf=[CCAABBBB], pos=2, full=true
	// Wait no, the AAAA is already overwritten by wrapping.
	// After AAAA+BBBB: buf=[AAAABBBB], pos=0, full=true
	// After CC: buf=[CCAABBBB], pos=2, full=true
	// Bytes(): from pos=2 to end + start to pos=2 = "AABBBB" + "CC" = "AABBBBCC"
	// Hmm, that's not right either. Let me think.
	// After AAAA: [A,A,A,A,_,_,_,_] pos=4
	// After BBBB: [A,A,A,A,B,B,B,B] pos=0, full=true
	// After CC:   [C,C,A,A,B,B,B,B] pos=2, full=true
	// Bytes: from pos(2)..end = AABBBB, then 0..pos(2) = CC -> "AABBBBCC"
	// But chronologically: AAAA then BBBB then CC = AAAABBBBCC, last 8 = AABBBBCC. Correct!
	if got != "AABBBBCC" {
		t.Errorf("got %q, want %q", got, "AABBBBCC")
	}
}

func TestRingBuffer_Len(t *testing.T) {
	r := NewRingBuffer(10)

	if r.Len() != 0 {
		t.Errorf("empty buffer Len: got %d, want 0", r.Len())
	}

	r.Write([]byte("hello"))
	if r.Len() != 5 {
		t.Errorf("after 5 bytes Len: got %d, want 5", r.Len())
	}

	r.Write([]byte("1234567890"))
	if r.Len() != 10 {
		t.Errorf("after overflow Len: got %d, want 10", r.Len())
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	r := NewRingBuffer(10)
	got := r.Bytes()
	if len(got) != 0 {
		t.Errorf("empty buffer should return empty bytes, got %d", len(got))
	}
}
