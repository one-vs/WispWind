package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"wispwind/internal/api"
	"wispwind/internal/audio"
	"wispwind/internal/config"
	"wispwind/internal/db"
	"wispwind/internal/focus"
	"wispwind/internal/hotkey"
	"wispwind/internal/llm"
	"wispwind/internal/paste"
	"wispwind/internal/storage"
	"wispwind/internal/stt"
	"wispwind/internal/usage"
	"wispwind/internal/widget"

	"github.com/getlantern/systray"
)

func main() {
	cleanup := ensureSingleInstance()
	defer cleanup()

	store, err := storage.New()
	if err != nil {
		log.Fatalf("Storage init failed: %v", err)
	}
	defer store.Close()

	database, err := db.New(store.UsageDir())
	if err != nil {
		log.Fatalf("DB init failed: %v", err)
	}
	defer database.Close()

	if err := store.ConfigureLogger(); err != nil {
		log.Fatalf("Logger init failed: %v", err)
	}

	cfg := config.Load(database)
	cfgHolder := config.NewHolder(database, cfg)

	adminURL, err := api.Start(database, store.LogsDir(), store.HistoryDir(), func() {
		cfgHolder.Reload()
		next := cfgHolder.Get()
		log.Printf("Settings reloaded | %s: %s | LLM: %s", next.Provider, next.Model, llmStatus(next.DisableLLM))
	})
	if err != nil {
		log.Printf("Failed to start Admin API: %v", err)
	} else {
		log.Printf("Admin panel started at %s", adminURL)
	}

	if err := paste.Init(); err != nil {
		log.Fatalf("Clipboard init failed: %v", err)
	}

	// Print startup info
	apiKey := cfg.OpenAIKey
	if cfg.Provider == "deepgram" {
		apiKey = cfg.DeepgramKey
	}
	maskedKey := maskAPIKey(apiKey)

	llmStatus := "Disabled"
	if !cfg.DisableLLM {
		llmStatus = "Enabled"
	}
	hotkeyInfo := fmt.Sprintf("%s (%s)", cfg.HotkeyStart, cfg.HotkeyMode)
	if cfg.HotkeyMode == "toggle" {
		hotkeyInfo = fmt.Sprintf("Start: %s, Stop: %s", cfg.HotkeyStart, cfg.HotkeyStop)
	}

	log.Printf("Starting WispWind | %s: %s (%s) | Key: %s | Hotkey: %s | LLM: %s",
		cfg.Provider, cfg.Model, cfg.STTMode, maskedKey, hotkeyInfo, llmStatus)

	if !requestRuntimePermissions() {
		log.Printf("Accessibility permission is not granted yet; smart paste may be limited until WispWind is enabled in Privacy & Security")
	}

	if err := audio.Init(); err != nil {
		log.Fatalf("Audio init failed: %v", err)
	}
	defer audio.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var currentCancel context.CancelFunc
	var cancelMu sync.Mutex

	// Graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		systray.Quit()
	}()

	systray.Run(func() {
		onReady(ctx, cfgHolder, database, adminURL, func(c context.CancelFunc) {
			cancelMu.Lock()
			currentCancel = c
			cancelMu.Unlock()
		}, func() {
			cancelMu.Lock()
			if currentCancel != nil {
				currentCancel()
				currentCancel = nil
			}
			cancelMu.Unlock()
		})
	}, onExit)
}

func onReady(ctx context.Context, cfgHolder *config.Holder, database *db.DB, adminURL string, setCancel func(context.CancelFunc), clearCancel func()) {
	cfg := cfgHolder.Get()
	bootSTTMode := cfg.STTMode
	systray.SetTitle("")
	systray.SetTooltip("Voice dictation")
	setTrayRecording(false)
	widget.Start()
	usageItem, lifetimeItem, modelItem := setupTrayMenu(cfg, database, adminURL)
	cfgHolder.OnChange(func(c *config.Config) {
		modelItem.SetTitle(fmt.Sprintf("Model: %s", c.Model))
	})

	type transcriptResult struct {
		Text  string
		Live  bool
		Final bool
	}

	resultChan := make(chan transcriptResult, 32)
	var targetMu sync.Mutex
	var targetWindow focus.Handle
	var targetPasteContext paste.SmartContext

	// Pipeline (Result -> LLM -> Paste)
	go func() {
		liveWriter := paste.NewLiveWriter()
		var pasteMu sync.Mutex
		var lastLiveText string
		var lastLiveAt time.Time

		for text := range resultChan {
			// Create a cancellable context for this specific processing task
			procCtx, procCancel := context.WithCancel(ctx)
			setCancel(procCancel)

			if strings.TrimSpace(text.Text) == "" {
				if text.Final {
					widget.Hide()
					setTrayRecording(false)
				}
				clearCancel()
				procCancel()
				continue
			}

			log.Printf("STT Result: %s\n", text.Text)

			if text.Live && !text.Final {
				pasteMu.Lock()
				if text.Text != lastLiveText && time.Since(lastLiveAt) >= 250*time.Millisecond {
					targetMu.Lock()
					smartCtx := targetPasteContext
					targetMu.Unlock()
					liveWriter.SetSmartContext(smartCtx)
					liveWriter.Replace(text.Text)
					lastLiveText = text.Text
					lastLiveAt = time.Now()
				}
				pasteMu.Unlock()
				clearCancel()
				procCancel()
				continue
			}

			finalText := text.Text
			c := cfgHolder.Get()
			if text.Final && !c.DisableLLM {
				llmStarted := time.Now()
				llmResult, err := llm.ProcessText(procCtx, c.OpenAIKey, c.Prompt, text.Text)
				if err != nil {
					if procCtx.Err() == context.Canceled {
						log.Printf("LLM cancelled after %s", time.Since(llmStarted).Round(time.Millisecond))
						clearCancel()
						procCancel()
						continue
					}
					// LLM failed — log and fall through to paste the raw transcript
					// so the user never loses their dictation.
					log.Printf("LLM Error after %s: %v", time.Since(llmStarted).Round(time.Millisecond), err)
				} else {
					finalText = llmResult.Text
					appendUsage(database, usageItem, lifetimeItem, usage.Record{
						Time:      time.Now(),
						Kind:      "llm",
						Provider:  "openai",
						Model:     "gpt-4o-mini",
						TextChars: len([]rune(finalText)),
						ElapsedMS: time.Since(llmStarted).Milliseconds(),
						Usage:     llmResult.Usage,
						CostUSD:   llmCost(c, llmResult.Usage),
						Text:      finalText,
					})
					log.Printf("LLM completed in %s", time.Since(llmStarted).Round(time.Millisecond))
				}
			}

			if procCtx.Err() == context.Canceled {
				clearCancel()
				procCancel()
				continue
			}

			pasteMu.Lock()
			widget.Hide()
			setTrayRecording(false)
			targetMu.Lock()
			tw := targetWindow
			smartCtx := targetPasteContext
			targetMu.Unlock()
			focus.Restore(tw)
			time.Sleep(120 * time.Millisecond)
			if text.Live {
				liveWriter.SetSmartContext(smartCtx)
				liveWriter.Replace(finalText)
				lastLiveText = finalText
				lastLiveAt = time.Time{}
				liveWriter.Forget()
				lastLiveText = ""
			} else {
				paste.PasteTextSmartWithContext(finalText, smartCtx)
			}
			pasteMu.Unlock()
			clearCancel()
			procCancel()
		}
	}()

	processor := make(chan []byte, 10)
	go func() {
		for wavData := range processor {
			// Create a cancellable context for this specific transcription task
			procCtx, procCancel := context.WithCancel(ctx)
			setCancel(procCancel)

			transcribeStarted := time.Now()
			c := cfgHolder.Get()
			var apiKey string
			if c.Provider == "deepgram" {
				apiKey = c.DeepgramKey
			} else {
				apiKey = c.OpenAIKey
			}
			result, err := stt.Transcribe(procCtx, c.Provider, c.Model, apiKey, c.Language, c.STTPrompt, wavData)
			if err != nil {
				if procCtx.Err() == context.Canceled {
					log.Printf("STT cancelled after %s", time.Since(transcribeStarted).Round(time.Millisecond))
				} else {
					log.Printf("STT Error after %s: %v", time.Since(transcribeStarted).Round(time.Millisecond), err)
				}
				widget.Hide()
				setTrayRecording(false)
				clearCancel()
				procCancel()
				continue
			}
			durationSeconds := estimateWAVDurationSeconds(wavData)
			appendUsage(database, usageItem, lifetimeItem, usage.Record{
				Time:            time.Now(),
				Kind:            "stt",
				Provider:        c.Provider,
				Model:           c.Model,
				DurationSeconds: durationSeconds,
				AudioBytes:      len(wavData),
				TextChars:       len([]rune(result.Text)),
				ElapsedMS:       time.Since(transcribeStarted).Milliseconds(),
				Usage:           result.Usage,
				CostUSD:         sttCost(c, result.Usage, durationSeconds),
				Text:            result.Text,
			})
			log.Printf("STT completed in %s, duration: %.1fs, chars: %d, cost: $%.6f", time.Since(transcribeStarted).Round(time.Millisecond), durationSeconds, len([]rune(result.Text)), sttCost(c, result.Usage, durationSeconds))
			
			if procCtx.Err() != context.Canceled {
				resultChan <- transcriptResult{Text: result.Text, Final: true}
			}
			clearCancel()
			procCancel()
		}
	}()

	var rtSession *stt.RealtimeSTT
	var rtMu sync.Mutex

	// Listen for Global Hotkeys
	go hotkey.Listen(
		hotkey.Config{
			Mode:  cfg.HotkeyMode,
			Start: cfg.HotkeyStart,
			Stop:  cfg.HotkeyStop,
		},
		func() {
			setTrayRecording(true)
			target := focus.Current()
			smartCtx := paste.CaptureContextForPID(int32(target))
			targetMu.Lock()
			targetWindow = target
			targetPasteContext = smartCtx
			targetMu.Unlock()
			widget.Show("listening")
			if bootSTTMode == "realtime" {
				if err := stt.ValidateRealtimeSampleRate(audio.SampleRate); err != nil {
					log.Printf("Realtime STT Config Error: %v", err)
					widget.Hide()
					setTrayRecording(false)
					return
				}
				c := cfgHolder.Get()
				rtMu.Lock()
				var err error
				rtSession, err = stt.NewRealtimeSTT(ctx, c.OpenAIKey, c.Model, c.Language, c.STTPrompt, audio.SampleRate, func(result stt.RealtimeResult) {
					resultChan <- transcriptResult{Text: result.Text, Live: true, Final: result.Final}
				})
				rtMu.Unlock()
				if err != nil {
					log.Printf("Realtime STT Init Error: %v", err)
					widget.Hide()
					setTrayRecording(false)
					return
				}
				if err := audio.StartRecording(func(pcm []int16) {
					rtMu.Lock()
					if rtSession != nil {
						if err := rtSession.PushAudio(pcm); err != nil {
							log.Printf("Realtime audio send error: %v", err)
						}
					}
					rtMu.Unlock()

					// Volume indicator
					level := audio.CalculateRMS(pcm)
					widget.SetLevel(level)
					bars := int(level * 100)
					if bars > 30 {
						bars = 30
					}
					status := "Listening"
					if level > 0.05 {
						status = "Speaking "
					}
					fmt.Printf("\r🎙️  [%-10s] [%-30s]", status, strings.Repeat("█", bars))
				}); err != nil {
					log.Printf("Audio start error: %v", err)
					widget.Hide()
					setTrayRecording(false)
				}
			} else {
				if err := audio.StartRecording(func(pcm []int16) {
					level := audio.CalculateRMS(pcm)
					widget.SetLevel(level)
					bars := int(level * 100)
					if bars > 30 {
						bars = 30
					}
					status := "Listening"
					if level > 0.05 {
						status = "Speaking "
					}
					fmt.Printf("\r🎙️  [%-10s] [%-30s]", status, strings.Repeat("█", bars))
				}); err != nil {
					log.Printf("Audio start error: %v", err)
					widget.Hide()
					setTrayRecording(false)
				}
			}
		},
		func() {
			fmt.Println() // New line after recording stops
			widget.SetStatus("processing")
			if bootSTTMode == "realtime" {
				rtMu.Lock()
				if rtSession != nil {
					audio.StopRecording()
					if err := rtSession.Commit(); err != nil {
						log.Printf("Realtime commit error: %v", err)
						widget.Hide()
						setTrayRecording(false)
					}
					sessionToClose := rtSession
					go func() {
						time.Sleep(8 * time.Second)
						sessionToClose.Close()
					}()
					rtSession = nil
				} else {
					widget.Hide()
					setTrayRecording(false)
				}
				rtMu.Unlock()
			} else {
				wavData := audio.StopRecording()
				if len(wavData) > 0 {
					processor <- wavData
				} else {
					widget.Hide()
					setTrayRecording(false)
				}
			}
		},
		func() { // onCancel
			fmt.Println()
			clearCancel()
			if bootSTTMode == "realtime" {
				rtMu.Lock()
				if rtSession != nil {
					audio.StopRecording()
					rtSession.Close()
					rtSession = nil
				} else {
					audio.StopRecording()
				}
				rtMu.Unlock()
			} else {
				audio.StopRecording()
			}
			widget.Hide()
			setTrayRecording(false)
		},
	)
}

func onExit() {
	log.Println("Exiting application...")
	os.Exit(0)
}

func setupTrayMenu(cfg *config.Config, database *db.DB, adminURL string) (*systray.MenuItem, *systray.MenuItem, *systray.MenuItem) {
	systray.AddMenuItem("WispWind", "Application status").Disable()
	modelItem := addInfoItem("Model", cfg.Model)
	usageItem := addInfoItem("Today", usageSummaryTitle(database))
	lifetimeItem := addInfoItem("Lifetime", usageSummaryTitleAllTime(database))

	systray.AddSeparator()
	openAdmin := systray.AddMenuItem("Open Dashboard", "Open the admin panel in your browser")

	go func() {
		for range openAdmin.ClickedCh {
			if adminURL != "" {
				if err := openPath(adminURL); err != nil {
					log.Printf("Failed to open admin URL: %v", err)
				}
			}
		}
	}()

	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Exit application")
	go func() {
		<-quitItem.ClickedCh
		systray.Quit()
	}()
	return usageItem, lifetimeItem, modelItem
}

func setTrayRecording(recording bool) {
	systray.SetIcon(getTrayIcon(recording))
}

func appendUsage(database *db.DB, todayItem, lifetimeItem *systray.MenuItem, record usage.Record) {
	if err := database.InsertUsage(context.Background(), record); err != nil {
		log.Printf("DB usage insert error: %v", err)
	}
	updateUsageMenuItem(database, todayItem, lifetimeItem)
}

func updateUsageMenuItem(database *db.DB, todayItem, lifetimeItem *systray.MenuItem) {
	if todayItem != nil {
		todayItem.SetTitle("Today: " + usageSummaryTitle(database))
		todayItem.Disable()
	}
	if lifetimeItem != nil {
		lifetimeItem.SetTitle("Lifetime: " + usageSummaryTitleAllTime(database))
		lifetimeItem.Disable()
	}
}

func usageSummaryTitle(database *db.DB) string {
	records, err := database.GetTodayUsage(context.Background())
	if err != nil {
		log.Printf("Usage summary error: %v", err)
		return "usage unavailable"
	}
	var count int
	var duration float64
	var cost float64
	for _, r := range records {
		count++
		duration += r.DurationSeconds
		cost += r.CostUSD
	}
	return fmt.Sprintf("%s, $%.4f, %d calls", formatDuration(duration), cost, count)
}

func usageSummaryTitleAllTime(database *db.DB) string {
	records, err := database.GetAllTimeUsage(context.Background())
	if err != nil {
		log.Printf("All-time usage summary error: %v", err)
		return "usage unavailable"
	}
	var count int
	var duration float64
	var cost float64
	for _, r := range records {
		count++
		duration += r.DurationSeconds
		cost += r.CostUSD
	}
	return fmt.Sprintf("%s, $%.4f, %d calls", formatDuration(duration), cost, count)
}

func sttCost(cfg *config.Config, u usage.TokenUsage, durationSeconds float64) float64 {
	r := cfg.CostRates
	if u.AudioTokens == 0 && u.TextTokens == 0 && u.OutputTokens == 0 && r.STTAudioPerMinute > 0 {
		return durationSeconds / 60 * r.STTAudioPerMinute
	}
	return float64(u.AudioTokens)/1_000_000*r.STTAudioInputPer1M +
		float64(u.TextTokens)/1_000_000*r.STTTextInputPer1M +
		float64(u.OutputTokens)/1_000_000*r.STTOutputPer1M
}

func llmCost(cfg *config.Config, u usage.TokenUsage) float64 {
	r := cfg.CostRates
	return float64(u.InputTokens)/1_000_000*r.LLMInputPer1M +
		float64(u.OutputTokens)/1_000_000*r.LLMOutputPer1M
}

func estimateWAVDurationSeconds(wavData []byte) float64 {
	if len(wavData) <= 44 {
		return 0
	}
	const bytesPerSecond = audio.SampleRate * 2
	return float64(len(wavData)-44) / float64(bytesPerSecond)
}

func formatDuration(seconds float64) string {
	total := int(seconds + 0.5)
	return fmt.Sprintf("%d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

func addInfoItem(label, value string) *systray.MenuItem {
	item := systray.AddMenuItem(fmt.Sprintf("%s: %s", label, value), "")
	item.Disable()
	return item
}

func openPathOnClick(ch <-chan struct{}, path func() string, beforeOpen func() error) {
	for range ch {
		if beforeOpen != nil {
			if err := beforeOpen(); err != nil {
				log.Printf("Open path preparation failed: %v", err)
				continue
			}
		}
		if err := openPath(path()); err != nil {
			log.Printf("Open path failed: %v", err)
		}
	}
}


func currentMaskedKey(cfg *config.Config) string {
	if cfg.Provider == "deepgram" {
		return maskAPIKey(cfg.DeepgramKey)
	}
	return maskAPIKey(cfg.OpenAIKey)
}

func maskAPIKey(apiKey string) string {
	if len(apiKey) > 17 {
		return apiKey[:12] + "..." + apiKey[len(apiKey)-5:]
	}
	return apiKey
}

func valueOrAuto(value string) string {
	if value == "" {
		return "auto"
	}
	return value
}

func llmStatus(disabled bool) string {
	if disabled {
		return "Disabled"
	}
	return "Enabled (gpt-4o)"
}
