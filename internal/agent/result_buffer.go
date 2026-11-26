package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nkb-diskless/internal/models"
)

const (
	// maxBufferCapacity limits memory and disk consumption during prolonged offline periods.
	maxBufferCapacity = 500
)

// ResultBuffer persists pending command results locally when the management server is unreachable.
type ResultBuffer struct {
	filePath string
	mu       sync.Mutex
	items    []models.CommandResult
}

// ResolveBufferPath establishes the path for storing offline command execution results.
func ResolveBufferPath() string {
	pocDir := `C:\NKB-PoC\config`
	if fi, err := os.Stat(`C:\NKB-PoC`); err == nil && fi.IsDir() {
		_ = os.MkdirAll(pocDir, 0755)
		return filepath.Join(pocDir, "pending_results.json")
	}

	programData := os.Getenv("ProgramData")
	if programData != "" {
		dir := filepath.Join(programData, "NKB_Agent")
		_ = os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "pending_results.json")
	}

	_ = os.MkdirAll("config", 0755)
	return filepath.Join("config", "pending_results.json")
}

// NewResultBuffer initializes or loads the pending results queue from disk.
func NewResultBuffer(customPath string) *ResultBuffer {
	if customPath == "" {
		customPath = ResolveBufferPath()
	}

	buf := &ResultBuffer{
		filePath: customPath,
		items:    make([]models.CommandResult, 0),
	}

	_ = buf.Load()
	return buf
}

// Enqueue adds a command result to the buffer and atomically persists it.
func (b *ResultBuffer) Enqueue(res *models.CommandResult) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Update existing result if commandID already queued, otherwise append
	found := false
	for i, item := range b.items {
		if item.CommandID == res.CommandID {
			b.items[i] = *res
			found = true
			break
		}
	}
	if !found {
		// Enforce capacity bounds
		if len(b.items) >= maxBufferCapacity {
			// Evict oldest result to prevent unbounded disk/memory growth
			slog.Warn("Offline buffer reached maximum capacity; evicting oldest entry", "limit", maxBufferCapacity)
			b.items = b.items[1:]
		}
		b.items = append(b.items, *res)
	}

	return b.saveLocked()
}

// GetAll returns a copy of all pending results currently queued.
func (b *ResultBuffer) GetAll() []models.CommandResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	copied := make([]models.CommandResult, len(b.items))
	copy(copied, b.items)
	return copied
}

// Remove deletes a successfully acknowledged result from the buffer and saves.
func (b *ResultBuffer) Remove(commandID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	newItems := make([]models.CommandResult, 0, len(b.items))
	for _, item := range b.items {
		if item.CommandID != commandID {
			newItems = append(newItems, item)
		}
	}
	b.items = newItems

	return b.saveLocked()
}

// Count returns the number of buffered items.
func (b *ResultBuffer) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// Load reads and parses buffered results. If corrupt, renames to corrupt backup and recovers cleanly.
func (b *ResultBuffer) Load() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	data, err := os.ReadFile(b.filePath)
	if os.IsNotExist(err) {
		b.items = make([]models.CommandResult, 0)
		return nil
	}
	if err != nil {
		return err
	}

	if len(data) == 0 {
		b.items = make([]models.CommandResult, 0)
		return nil
	}

	var loaded []models.CommandResult
	if err := json.Unmarshal(data, &loaded); err != nil {
		// File corrupted: preserve copy for forensics and recover cleanly
		corruptName := fmt.Sprintf("%s.corrupt_%d.json", b.filePath, time.Now().UnixNano())
		_ = os.Rename(b.filePath, corruptName)
		slog.Error("Offline results buffer corrupted; preserved backup copy and recovered",
			"corrupt_file", corruptName, "err", err)
		b.items = make([]models.CommandResult, 0)
		return nil
	}

	b.items = loaded
	return nil
}

// saveLocked atomically persists the items to disk using a temporary file with restricted permissions.
func (b *ResultBuffer) saveLocked() error {
	dir := filepath.Dir(b.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(b.items, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := b.filePath + ".tmp"
	// Restrict file permissions to 0600 (owner read/write only)
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	_ = os.Remove(b.filePath)
	if err := os.Rename(tmpPath, b.filePath); err != nil {
		// Fallback direct write
		return os.WriteFile(b.filePath, data, 0600)
	}

	return nil
}
