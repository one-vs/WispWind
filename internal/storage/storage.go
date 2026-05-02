package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"wispwind/internal/usage"
)

type Store struct {
	baseDir string
	logFile *os.File
	mu      sync.Mutex
}

type syncFileWriter struct {
	file *os.File
}

func (w syncFileWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err == nil {
		_ = w.file.Sync()
	}
	return n, err
}

func New() (*Store, error) {
	baseDir, err := appDir()
	if err != nil {
		return nil, err
	}
	s := &Store{baseDir: baseDir}
	if err := os.MkdirAll(s.LogsDir(), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.HistoryDir(), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.UsageDir(), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) LogsDir() string {
	return filepath.Join(s.baseDir, "logs")
}

func (s *Store) HistoryDir() string {
	return filepath.Join(s.baseDir, "history")
}

func (s *Store) UsageDir() string {
	return filepath.Join(s.baseDir, "usage")
}

func (s *Store) TodayLogPath() string {
	return filepath.Join(s.LogsDir(), time.Now().Format("2006-01-02")+".log")
}

func (s *Store) TodayHistoryPath() string {
	return filepath.Join(s.HistoryDir(), time.Now().Format("2006-01-02")+".md")
}

func (s *Store) TodayUsagePath() string {
	return filepath.Join(s.UsageDir(), time.Now().Format("2006-01-02")+".json")
}

func (s *Store) EnsureTodayHistory() error {
	path := s.TodayHistoryPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *Store) ConfigureLogger() error {
	path := s.TodayLogPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.logFile = f
	log.SetOutput(io.MultiWriter(syncFileWriter{file: f}, os.Stderr))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("Log file: %s", path)
	_ = f.Sync()
	return nil
}

func (s *Store) Close() {
	if s.logFile != nil {
		s.logFile.Close()
	}
}

func (s *Store) AppendTranscript(text string) error {
	if text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	path := s.TodayHistoryPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "## %s\n\n%s\n\n", now.Format("15:04:05"), text)
	return err
}

func (s *Store) AppendUsage(record usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.Time.IsZero() {
		record.Time = time.Now()
	}

	path := s.TodayUsagePath()
	var day usage.DayUsage

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &day)
	}

	day.Records = append(day.Records, record)
	day.Total.Records++
	day.Total.DurationSeconds += record.DurationSeconds
	day.Total.DurationMinutes = day.Total.DurationSeconds / 60.0
	day.Total.CostUSD += record.CostUSD
	day.Total.InputTokens += record.Usage.InputTokens
	day.Total.TextTokens += record.Usage.TextTokens
	day.Total.AudioTokens += record.Usage.AudioTokens
	day.Total.OutputTokens += record.Usage.OutputTokens
	day.Total.TotalTokens += record.Usage.TotalTokens

	b, err := json.MarshalIndent(day, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0o644)
}

func (s *Store) TodayUsageSummary() (usage.Summary, error) {
	path := s.TodayUsagePath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return usage.Summary{}, nil
	}
	if err != nil {
		return usage.Summary{}, err
	}

	var day usage.DayUsage
	if err := json.Unmarshal(data, &day); err != nil {
		return usage.Summary{}, err
	}
	return day.Total, nil
}

func appDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
