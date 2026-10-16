package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Task struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	Steps       []Step    `json:"steps"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Step struct {
	Index       int       `json:"index"`
	Thinking    string    `json:"thinking"`
	Action      string    `json:"action"`
	Selector    string    `json:"selector,omitempty"`
	Value       string    `json:"value,omitempty"`
	Description string    `json:"description"`
	Screenshot  string    `json:"screenshot,omitempty"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

type TaskStore struct {
	mu      sync.RWMutex
	dataDir string
}

func NewTaskStore(dataDir string) *TaskStore {
	os.MkdirAll(filepath.Join(dataDir, "tasks"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "screenshots"), 0755)
	return &TaskStore{dataDir: dataDir}
}

func (s *TaskStore) Save(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.taskPath(t.ID), data, 0644)
}

func (s *TaskStore) Get(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.taskPath(id))
	if err != nil {
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *TaskStore) List() ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := filepath.Join(s.dataDir, "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []*Task
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t Task
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		tasks = append(tasks, &t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (s *TaskStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	os.Remove(s.taskPath(id))
	matches, _ := filepath.Glob(filepath.Join(s.dataDir, "screenshots", id+"_*"))
	for _, m := range matches {
		os.Remove(m)
	}
	return nil
}

func (s *TaskStore) ScreenshotDir() string {
	return filepath.Join(s.dataDir, "screenshots")
}

func (s *TaskStore) taskPath(id string) string {
	return filepath.Join(s.dataDir, "tasks", id+".json")
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
