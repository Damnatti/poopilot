package pty

import (
	"sync"
	"testing"
	"time"
)

func TestManager_Create_AssignsUniqueIDs(t *testing.T) {
	m := NewManager(8)
	defer m.CloseAll()

	s1, err := m.Create("echo", []string{"a"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	s2, err := m.Create("echo", []string{"b"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if s1.ID == s2.ID {
		t.Errorf("sessions should have unique IDs")
	}
}

func TestManager_Create_RejectsOverLimit(t *testing.T) {
	m := NewManager(2)
	defer m.CloseAll()

	_, err := m.Create("sleep", []string{"60"})
	if err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	_, err = m.Create("sleep", []string{"60"})
	if err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}
	_, err = m.Create("sleep", []string{"60"})
	if err == nil {
		t.Error("Create 3 should fail (over limit)")
	}
}

func TestManager_Get_ReturnsSession(t *testing.T) {
	m := NewManager(8)
	defer m.CloseAll()

	s, _ := m.Create("echo", []string{"hello"})

	got, ok := m.Get(s.ID)
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.ID != s.ID {
		t.Errorf("ID mismatch")
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	m := NewManager(8)
	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("Get should return false for unknown ID")
	}
}

func TestManager_List(t *testing.T) {
	m := NewManager(8)
	defer m.CloseAll()

	m.Create("sleep", []string{"60"})
	m.Create("sleep", []string{"60"})

	infos := m.List()
	if len(infos) != 2 {
		t.Errorf("List: got %d sessions, want 2", len(infos))
	}
}

func TestManager_Close_RemovesSession(t *testing.T) {
	m := NewManager(8)
	defer m.CloseAll()

	s, _ := m.Create("sleep", []string{"60"})
	err := m.Close(s.ID)
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, ok := m.Get(s.ID)
	if ok {
		t.Error("session should be removed after Close")
	}
	if m.Count() != 0 {
		t.Errorf("Count: got %d, want 0", m.Count())
	}
}

func TestManager_Close_NotFound(t *testing.T) {
	m := NewManager(8)
	err := m.Close("nonexistent")
	if err == nil {
		t.Error("Close should fail for unknown ID")
	}
}

func TestManager_CloseAll(t *testing.T) {
	m := NewManager(8)
	m.Create("sleep", []string{"60"})
	m.Create("sleep", []string{"60"})
	m.Create("sleep", []string{"60"})

	m.CloseAll()

	if m.Count() != 0 {
		t.Errorf("after CloseAll: Count = %d, want 0", m.Count())
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager(100)
	defer m.CloseAll()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := m.Create("echo", []string{"concurrent"})
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			m.List()
			m.Get(s.ID)
		}()
	}

	wg.Wait()
	// No race condition = pass
}

func TestManager_Count(t *testing.T) {
	m := NewManager(8)
	defer m.CloseAll()

	if m.Count() != 0 {
		t.Errorf("initial Count: got %d, want 0", m.Count())
	}

	m.Create("sleep", []string{"60"})
	if m.Count() != 1 {
		t.Errorf("after 1 create: got %d, want 1", m.Count())
	}

	m.Create("sleep", []string{"60"})
	if m.Count() != 2 {
		t.Errorf("after 2 creates: got %d, want 2", m.Count())
	}
}
