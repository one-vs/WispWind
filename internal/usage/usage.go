package usage

import "time"

type TokenUsage struct {
	Type         string `json:"type,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	TextTokens   int    `json:"text_tokens,omitempty"`
	AudioTokens  int    `json:"audio_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	TotalTokens  int    `json:"total_tokens,omitempty"`
}

type Record struct {
	Time            time.Time  `json:"time"`
	Kind            string     `json:"kind"`
	Provider        string     `json:"provider,omitempty"`
	Model           string     `json:"model"`
	DurationSeconds float64    `json:"duration_seconds,omitempty"`
	AudioBytes      int        `json:"audio_bytes,omitempty"`
	TextChars       int        `json:"text_chars,omitempty"`
	ElapsedMS       int64      `json:"elapsed_ms,omitempty"`
	Usage           TokenUsage `json:"usage"`
	CostUSD         float64    `json:"cost_usd"`
	Text            string     `json:"text,omitempty"`
}

type Summary struct {
	Records         int     `json:"records"`
	DurationSeconds float64 `json:"duration_seconds"`
	DurationMinutes float64 `json:"duration_minutes"`
	CostUSD         float64 `json:"cost_usd"`
	InputTokens     int     `json:"input_tokens"`
	TextTokens      int     `json:"text_tokens"`
	AudioTokens     int     `json:"audio_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	TotalTokens     int     `json:"total_tokens"`
}

type DayUsage struct {
	Total   Summary  `json:"total"`
	Records []Record `json:"records"`
}
