package eta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStore keeps history in one JSON file, by default ~/.cluster/history.json.
// Local for v0; the interface is what makes a shared store a later change rather
// than a rewrite.
type FileStore struct {
	Path string

	mu   sync.Mutex
	data map[string][]int64 // key -> durations in nanoseconds, oldest first
}

// DefaultPath is where dev runs accumulate history, so the estimate is calibrated
// for this machine after a week of ordinary work.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cluster-history.json"
	}
	return filepath.Join(home, ".cluster", "history.json")
}

func NewFileStore(path string) *FileStore {
	if path == "" {
		path = DefaultPath()
	}
	return &FileStore{Path: path}
}

func (s *FileStore) Append(k Key, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	key := k.String()
	s.data[key] = append(s.data[key], int64(d))
	if len(s.data[key]) > Window {
		s.data[key] = s.data[key][len(s.data[key])-Window:]
	}
	return s.save()
}

func (s *FileStore) Durations(k Key) ([]time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	raw := s.data[k.String()]
	out := make([]time.Duration, len(raw))
	for i, n := range raw {
		out[i] = time.Duration(n)
	}
	return out, nil
}

func (s *FileStore) load() error {
	if s.data != nil {
		return nil
	}
	s.data = map[string][]int64{}
	b, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// A corrupt history file must not stop a cluster from coming up: an estimate
	// is a convenience, so start over rather than fail the command.
	if err := json.Unmarshal(b, &s.data); err != nil {
		s.data = map[string][]int64{}
	}
	return nil
}

func (s *FileStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(b, '\n'), 0o644)
}

// MemStore is the store the pure tests use.
type MemStore struct {
	mu   sync.Mutex
	data map[string][]time.Duration
}

func NewMemStore() *MemStore { return &MemStore{data: map[string][]time.Duration{}} }

func (m *MemStore) Append(k Key, d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k.String()] = append(m.data[k.String()], d)
	return nil
}

func (m *MemStore) Durations(k Key) ([]time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[k.String()], nil
}
