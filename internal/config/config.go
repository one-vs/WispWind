package config

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"wispwind/internal/db"

	"github.com/joho/godotenv"
)

type Config struct {
	Provider      string
	Model         string
	OpenAIKey     string
	DeepgramKey   string
	STTMode       string
	Language      string
	STTPrompt     string
	Prompt        string
	DisableLLM    bool
	HotkeyMode    string
	HotkeyStart   string
	HotkeyStop    string
	HotkeyHistory string
	CostRates     CostRates

	// SmartSpacing enables leading-space detection via synthetic keystrokes
	// (has side effects in some apps; off by default).
	SmartSpacing bool
	// RestoreClipboard restores the previous clipboard content after pasting.
	RestoreClipboard bool
	// PasteMode: "clipboard" (Ctrl+V) or "type" (unicode SendInput).
	PasteMode string
	// MaxRecordSeconds force-stops a recording after this many seconds.
	MaxRecordSeconds int
	// WaveTheme is the widget wave color scheme: green, purple, yellow, red, blue.
	WaveTheme string
	// SaveRecordings keeps a WAV backup of each dictation before it is sent
	// to the STT API, so a failed transcription never loses the take.
	SaveRecordings bool
	// MicGain is "auto" (AGC) or a fixed multiplier like "3".
	MicGain string
}

type CostRates struct {
	STTAudioInputPer1M float64
	STTAudioPerMinute  float64
	STTTextInputPer1M  float64
	STTOutputPer1M     float64
	LLMInputPer1M      float64
	LLMOutputPer1M     float64
}

func getSetting(database *db.DB, key string, envKey string) string {
	if database != nil {
		if val, err := database.GetSetting(context.Background(), key); err == nil && val != "" {
			return val
		}
	}
	return cleanEnvValue(os.Getenv(envKey))
}

func Load(database *db.DB) *Config {
	_ = godotenv.Load(envPaths()...)

	provider := getSetting(database, "STT_PROVIDER", "STT_PROVIDER")
	if provider == "" {
		provider = "openai"
	}
	provider = strings.ToLower(provider)

	model := getSetting(database, "STT_MODEL", "STT_MODEL")
	if model == "" {
		if provider == "openai" {
			model = "gpt-4o-mini-transcribe"
		} else {
			model = "nova-2"
		}
	}

	openAIKey := getSetting(database, "OPENAI_API_KEY", "OPENAI_API_KEY")
	deepgramKey := getSetting(database, "DEEPGRAM", "DEEPGRAM")

	if provider == "openai" && openAIKey == "" {
		log.Fatal("OPENAI_API_KEY is required for openai provider")
	}
	if provider == "deepgram" && deepgramKey == "" {
		log.Fatal("DEEPGRAM is required for deepgram provider")
	}

	prompt := getSetting(database, "PROMPT", "PROMPT")
	if prompt == "" {
		prompt = "Transcribe clearly with correct punctuation, capitalization, and paragraphing. Preserve the spoken language and technical terms."
	}

	disableLLM := strings.EqualFold(getSetting(database, "DISABLE_LLM", "DISABLE_LLM"), "true")

	sttMode := getSetting(database, "STT_MODE", "STT_MODE")
	if sttMode == "" {
		sttMode = "batch"
	}
	sttMode = strings.ToLower(sttMode)
	if sttMode != "batch" && sttMode != "realtime" {
		log.Fatalf("STT_MODE must be batch or realtime, got %q", sttMode)
	}

	language := getSetting(database, "STT_LANGUAGE", "STT_LANGUAGE")
	sttPrompt := getSetting(database, "STT_PROMPT", "STT_PROMPT")
	hotkeyMode := strings.ToLower(getSetting(database, "HOTKEY_MODE", "HOTKEY_MODE"))
	if hotkeyMode == "" {
		hotkeyMode = "hold"
	}
	if hotkeyMode != "hold" && hotkeyMode != "toggle" {
		log.Fatalf("HOTKEY_MODE must be hold or toggle, got %q", hotkeyMode)
	}
	hotkeyStart := getSetting(database, "HOTKEY_START", "HOTKEY_START")
	if hotkeyStart == "" {
		hotkeyStart = "ctrl+space"
	}
	hotkeyStop := getSetting(database, "HOTKEY_STOP", "HOTKEY_STOP")
	if hotkeyStop == "" {
		hotkeyStop = "ctrl+shift+space"
	}
	hotkeyHistory := getSetting(database, "HOTKEY_HISTORY", "HOTKEY_HISTORY")
	if hotkeyHistory == "" {
		hotkeyHistory = "ctrl+space+z"
	}
	smartSpacing := strings.EqualFold(getSetting(database, "SMART_SPACING", "SMART_SPACING"), "true")
	restoreClipboard := !strings.EqualFold(getSetting(database, "RESTORE_CLIPBOARD", "RESTORE_CLIPBOARD"), "false")
	pasteMode := strings.ToLower(getSetting(database, "PASTE_MODE", "PASTE_MODE"))
	if pasteMode != "type" {
		pasteMode = "clipboard"
	}
	maxRecordSeconds := int(getFloatSetting(database, "MAX_RECORD_SECONDS", 300))
	if maxRecordSeconds <= 0 {
		maxRecordSeconds = 300
	}
	waveTheme := strings.ToLower(getSetting(database, "WAVE_THEME", "WAVE_THEME"))
	if waveTheme == "" {
		waveTheme = "green"
	}
	saveRecordings := !strings.EqualFold(getSetting(database, "SAVE_RECORDINGS", "SAVE_RECORDINGS"), "false")
	micGain := strings.ToLower(getSetting(database, "MIC_GAIN", "MIC_GAIN"))
	if micGain == "" {
		micGain = "auto"
	}

	costRates := defaultCostRates(model)
	costRates.STTAudioInputPer1M = getFloatSetting(database, "COST_STT_AUDIO_INPUT_USD_PER_1M", costRates.STTAudioInputPer1M)
	costRates.STTAudioPerMinute = getFloatSetting(database, "COST_STT_AUDIO_USD_PER_MINUTE", costRates.STTAudioPerMinute)
	costRates.STTTextInputPer1M = getFloatSetting(database, "COST_STT_TEXT_INPUT_USD_PER_1M", costRates.STTTextInputPer1M)
	costRates.STTOutputPer1M = getFloatSetting(database, "COST_STT_OUTPUT_USD_PER_1M", costRates.STTOutputPer1M)
	costRates.LLMInputPer1M = getFloatSetting(database, "COST_LLM_INPUT_USD_PER_1M", 0.15)
	costRates.LLMOutputPer1M = getFloatSetting(database, "COST_LLM_OUTPUT_USD_PER_1M", 0.60)

	if database != nil {
		ctx := context.Background()
		database.SaveSetting(ctx, "STT_PROVIDER", provider)
		database.SaveSetting(ctx, "STT_MODEL", model)
		database.SaveSetting(ctx, "OPENAI_API_KEY", openAIKey)
		database.SaveSetting(ctx, "DEEPGRAM", deepgramKey)
		database.SaveSetting(ctx, "STT_MODE", sttMode)
		database.SaveSetting(ctx, "STT_LANGUAGE", language)
		database.SaveSetting(ctx, "STT_PROMPT", sttPrompt)
		database.SaveSetting(ctx, "PROMPT", prompt)
		database.SaveSetting(ctx, "DISABLE_LLM", strconv.FormatBool(disableLLM))
		database.SaveSetting(ctx, "HOTKEY_MODE", hotkeyMode)
		database.SaveSetting(ctx, "HOTKEY_START", hotkeyStart)
		database.SaveSetting(ctx, "HOTKEY_STOP", hotkeyStop)
		database.SaveSetting(ctx, "HOTKEY_HISTORY", hotkeyHistory)
		database.SaveSetting(ctx, "SMART_SPACING", strconv.FormatBool(smartSpacing))
		database.SaveSetting(ctx, "RESTORE_CLIPBOARD", strconv.FormatBool(restoreClipboard))
		database.SaveSetting(ctx, "PASTE_MODE", pasteMode)
		database.SaveSetting(ctx, "MAX_RECORD_SECONDS", strconv.Itoa(maxRecordSeconds))
		database.SaveSetting(ctx, "WAVE_THEME", waveTheme)
		database.SaveSetting(ctx, "SAVE_RECORDINGS", strconv.FormatBool(saveRecordings))
		database.SaveSetting(ctx, "MIC_GAIN", micGain)

		// COST_* settings are intentionally NOT persisted here: a stored
		// value is treated as an explicit user override, otherwise the
		// model-based defaults above apply (and stay updatable in code).
	}

	return &Config{
		Provider:      provider,
		Model:         model,
		OpenAIKey:     openAIKey,
		DeepgramKey:   deepgramKey,
		STTMode:       sttMode,
		Language:      language,
		STTPrompt:     sttPrompt,
		Prompt:        prompt,
		DisableLLM:    disableLLM,
		HotkeyMode:    hotkeyMode,
		HotkeyStart:   hotkeyStart,
		HotkeyStop:    hotkeyStop,
		HotkeyHistory: hotkeyHistory,
		CostRates:     costRates,

		SmartSpacing:     smartSpacing,
		RestoreClipboard: restoreClipboard,
		PasteMode:        pasteMode,
		MaxRecordSeconds: maxRecordSeconds,
		WaveTheme:        waveTheme,
		SaveRecordings:   saveRecordings,
		MicGain:          micGain,
	}
}

// Holder stores the active *Config behind an atomic pointer so it can be
// swapped at runtime without locking readers.
type Holder struct {
	cur       atomic.Pointer[Config]
	db        *db.DB
	mu        sync.Mutex
	listeners []func(*Config)
}

func NewHolder(database *db.DB, cfg *Config) *Holder {
	h := &Holder{db: database}
	h.cur.Store(cfg)
	return h
}

func (h *Holder) Get() *Config { return h.cur.Load() }

// Reload re-reads hot-reloadable settings from the database and atomically
// swaps the current snapshot. Structural fields (STTMode, hotkey settings)
// are preserved from the previous snapshot since active listeners and
// sessions are bound to them at startup.
func (h *Holder) Reload() {
	next := ReloadHot(h.db, h.cur.Load())
	h.cur.Store(next)
	h.mu.Lock()
	listeners := append([]func(*Config){}, h.listeners...)
	h.mu.Unlock()
	for _, fn := range listeners {
		fn(next)
	}
}

func (h *Holder) OnChange(fn func(*Config)) {
	h.mu.Lock()
	h.listeners = append(h.listeners, fn)
	h.mu.Unlock()
}

// ReloadHot returns a new Config with hot-reloadable fields refreshed from the
// database and structural fields copied from current. Validation is skipped;
// missing or unparseable values fall back to the existing snapshot.
func ReloadHot(database *db.DB, current *Config) *Config {
	next := *current

	if v := getSetting(database, "STT_PROVIDER", "STT_PROVIDER"); v != "" {
		next.Provider = strings.ToLower(v)
	}
	if v := getSetting(database, "STT_MODEL", "STT_MODEL"); v != "" {
		next.Model = v
	}
	if v := getSetting(database, "OPENAI_API_KEY", "OPENAI_API_KEY"); v != "" {
		next.OpenAIKey = v
	}
	if v := getSetting(database, "DEEPGRAM", "DEEPGRAM"); v != "" {
		next.DeepgramKey = v
	}
	next.Language = getSetting(database, "STT_LANGUAGE", "STT_LANGUAGE")
	next.STTPrompt = getSetting(database, "STT_PROMPT", "STT_PROMPT")
	if v := getSetting(database, "PROMPT", "PROMPT"); v != "" {
		next.Prompt = v
	}
	next.DisableLLM = strings.EqualFold(getSetting(database, "DISABLE_LLM", "DISABLE_LLM"), "true")
	next.SmartSpacing = strings.EqualFold(getSetting(database, "SMART_SPACING", "SMART_SPACING"), "true")
	next.RestoreClipboard = !strings.EqualFold(getSetting(database, "RESTORE_CLIPBOARD", "RESTORE_CLIPBOARD"), "false")
	if v := strings.ToLower(getSetting(database, "PASTE_MODE", "PASTE_MODE")); v == "type" || v == "clipboard" {
		next.PasteMode = v
	}
	if v := strings.ToLower(getSetting(database, "WAVE_THEME", "WAVE_THEME")); v != "" {
		next.WaveTheme = v
	}
	next.SaveRecordings = !strings.EqualFold(getSetting(database, "SAVE_RECORDINGS", "SAVE_RECORDINGS"), "false")
	if v := strings.ToLower(getSetting(database, "MIC_GAIN", "MIC_GAIN")); v != "" {
		next.MicGain = v
	}

	rates := defaultCostRates(next.Model)
	rates.STTAudioInputPer1M = getFloatOr(database, "COST_STT_AUDIO_INPUT_USD_PER_1M", rates.STTAudioInputPer1M)
	rates.STTAudioPerMinute = getFloatOr(database, "COST_STT_AUDIO_USD_PER_MINUTE", rates.STTAudioPerMinute)
	rates.STTTextInputPer1M = getFloatOr(database, "COST_STT_TEXT_INPUT_USD_PER_1M", rates.STTTextInputPer1M)
	rates.STTOutputPer1M = getFloatOr(database, "COST_STT_OUTPUT_USD_PER_1M", rates.STTOutputPer1M)
	rates.LLMInputPer1M = getFloatOr(database, "COST_LLM_INPUT_USD_PER_1M", current.CostRates.LLMInputPer1M)
	rates.LLMOutputPer1M = getFloatOr(database, "COST_LLM_OUTPUT_USD_PER_1M", current.CostRates.LLMOutputPer1M)
	next.CostRates = rates

	return &next
}

func getFloatOr(database *db.DB, key string, fallback float64) float64 {
	value := getSetting(database, key, key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultCostRates(model string) CostRates {
	switch model {
	case "gpt-4o-mini-transcribe":
		return CostRates{STTAudioInputPer1M: 3.00, STTAudioPerMinute: 0.003, STTTextInputPer1M: 1.25, STTOutputPer1M: 5.00}
	case "gpt-4o-transcribe", "gpt-4o-transcribe-diarize":
		return CostRates{STTAudioInputPer1M: 6.00, STTAudioPerMinute: 0.006, STTTextInputPer1M: 2.50, STTOutputPer1M: 10.00}
	case "whisper-1":
		return CostRates{STTAudioPerMinute: 0.006}
	}
	// Deepgram models bill per minute of audio.
	if strings.HasPrefix(model, "nova") {
		return CostRates{STTAudioPerMinute: 0.0043}
	}
	return CostRates{}
}

func getFloatSetting(database *db.DB, key string, fallback float64) float64 {
	value := getSetting(database, key, key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		log.Fatalf("%s must be a number, got %q", key, value)
	}
	return parsed
}

func envPaths() []string {
	paths := []string{}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	paths = append(paths, ".env")
	return paths
}

func cleanEnvValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") {
		return ""
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return strings.Trim(v, `"'`)
}
