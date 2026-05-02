package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"wispwind/internal/usage"
)

type DB struct {
	db *sql.DB
}

func New(dir string) (*DB, error) {
	path := filepath.Join(dir, "wispwind.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := initSchema(db); err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}

func initSchema(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time DATETIME,
			kind TEXT,
			provider TEXT,
			model TEXT,
			duration_seconds REAL,
			audio_bytes INTEGER,
			text_chars INTEGER,
			elapsed_ms INTEGER,
			input_tokens INTEGER,
			text_tokens INTEGER,
			audio_tokens INTEGER,
			output_tokens INTEGER,
			total_tokens INTEGER,
			cost_usd REAL
		)`,
		`CREATE TABLE IF NOT EXISTS user_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time DATETIME,
			kind TEXT,
			text TEXT
		)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Migrate legacy history from usage_logs to user_history
	_, _ = db.Exec(`INSERT INTO user_history (time, kind, text) 
		SELECT time, kind, transcribed_text FROM usage_logs 
		WHERE transcribed_text IS NOT NULL AND transcribed_text != ''
		AND NOT EXISTS (
			SELECT 1 FROM user_history 
			WHERE user_history.time = usage_logs.time 
			AND user_history.text = usage_logs.transcribed_text
		)`)

	return nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) SaveSetting(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, value)
	return err
}

func (d *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := d.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (d *DB) GetAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, nil
}

func (d *DB) InsertUsage(ctx context.Context, record usage.Record) error {
	query := `INSERT INTO usage_logs (
		time, kind, provider, model, duration_seconds, audio_bytes, text_chars, elapsed_ms,
		input_tokens, text_tokens, audio_tokens, output_tokens, total_tokens, cost_usd
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.ExecContext(ctx, query,
		record.Time, record.Kind, record.Provider, record.Model, record.DurationSeconds,
		record.AudioBytes, record.TextChars, record.ElapsedMS,
		record.Usage.InputTokens, record.Usage.TextTokens, record.Usage.AudioTokens,
		record.Usage.OutputTokens, record.Usage.TotalTokens, record.CostUSD,
	)
	if err != nil {
		return err
	}

	if record.Text != "" {
		_, err = d.db.ExecContext(ctx, `INSERT INTO user_history (time, kind, text) VALUES (?, ?, ?)`, record.Time, record.Kind, record.Text)
	}

	return err
}

func (d *DB) GetTodayUsage(ctx context.Context) ([]usage.Record, error) {
	now := time.Now()
	todayStr := now.Format("2006-01-02")

	rows, err := d.db.QueryContext(ctx, `SELECT 
		time, kind, provider, model, duration_seconds, audio_bytes, text_chars, elapsed_ms,
		input_tokens, text_tokens, audio_tokens, output_tokens, total_tokens, cost_usd
		FROM usage_logs WHERE substr(time, 1, 10) = ? ORDER BY time ASC`, todayStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []usage.Record
	for rows.Next() {
		var r usage.Record
		var t time.Time
		if err := rows.Scan(
			&t, &r.Kind, &r.Provider, &r.Model, &r.DurationSeconds, &r.AudioBytes, &r.TextChars, &r.ElapsedMS,
			&r.Usage.InputTokens, &r.Usage.TextTokens, &r.Usage.AudioTokens, &r.Usage.OutputTokens, &r.Usage.TotalTokens, &r.CostUSD,
		); err != nil {
			return nil, err
		}
		r.Time = t
		records = append(records, r)
	}
	return records, nil
}

func (d *DB) GetHistoryDates(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT substr(time, 1, 10) FROM user_history ORDER BY substr(time, 1, 10) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, nil
}

func (d *DB) GetHistoryByDate(ctx context.Context, dateStr string) ([]usage.Record, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT time, kind, text FROM user_history WHERE substr(time, 1, 10) = ? ORDER BY time ASC`, dateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []usage.Record
	for rows.Next() {
		var r usage.Record
		var t time.Time
		var text string
		if err := rows.Scan(&t, &r.Kind, &text); err != nil {
			return nil, err
		}
		r.Time = t
		r.Text = text
		records = append(records, r)
	}
	return records, nil
}
