package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"wispwind/internal/api"
	"wispwind/internal/audio"
	"wispwind/internal/config"
	"wispwind/internal/db"
	"wispwind/internal/focus"
	"wispwind/internal/history"
	"wispwind/internal/hotkey"
	"wispwind/internal/llm"
	"wispwind/internal/paste"
	"wispwind/internal/storage"
	"wispwind/internal/stt"
	"wispwind/internal/trayicon"
	"wispwind/internal/usage"
	"wispwind/internal/widget"

	"github.com/getlantern/systray"
)

func main() {
	// Supervisor mode: the first process only spawns and watches the real
	// app. A crash (non-zero exit) is logged with its full panic stack to
	// logs/crash.log and the app is restarted automatically.
	if os.Getenv("WISPWIND_CHILD") != "1" {
		runSupervisor()
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex := kernel32.NewProc("CreateMutexW")
	mutexName, _ := syscall.UTF16PtrFromString("WispWind_SingleInstance_Mutex")
	handle, _, err := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if err != nil && err.(syscall.Errno) == 183 {
		os.Exit(0)
	}
	defer syscall.CloseHandle(syscall.Handle(handle))

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

	if migrated, err := database.MigrateCostDefaultsV2(context.Background()); err != nil {
		log.Printf("Cost defaults migration error: %v", err)
	} else if migrated {
		log.Printf("Cost rates corrected: stale defaults dropped, historical STT costs recomputed")
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
	applyPasteOptions := func(c *config.Config) {
		paste.Configure(paste.Options{
			SmartSpacing:     c.SmartSpacing,
			RestoreClipboard: c.RestoreClipboard,
			Mode:             c.PasteMode,
		})
	}
	applyPasteOptions(cfg)
	cfgHolder.OnChange(applyPasteOptions)

	store.PruneRecordings(7 * 24 * time.Hour)

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

	if err := audio.Init(); err != nil {
		log.Fatalf("Audio init failed: %v", err)
	}
	defer audio.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		systray.Quit()
	}()

	systray.Run(func() { onReady(ctx, cfgHolder, database, store, adminURL) }, onExit)
}

func onReady(ctx context.Context, cfgHolder *config.Holder, database *db.DB, store *storage.Store, adminURL string) {
	cfg := cfgHolder.Get()
	bootSTTMode := cfg.STTMode
	systray.SetTitle("VoiceTyping")
	systray.SetTooltip("Voice dictation")
	setTrayRecording(false)
	widget.Start()
	widget.SetTheme(cfg.WaveTheme)
	usageItem, lifetimeItem, modelItem := setupTrayMenu(cfg, database, adminURL)
	cfgHolder.OnChange(func(c *config.Config) {
		modelItem.SetTitle(fmt.Sprintf("Model: %s", c.Model))
		widget.SetTheme(c.WaveTheme)
	})

	type transcriptResult struct {
		Text  string
		Live  bool
		Final bool
	}

	resultChan := make(chan transcriptResult, 32)
	var targetMu sync.Mutex
	var targetWindow focus.Handle

	// pendingFinal guards the realtime path: if no final transcript arrives
	// within the watchdog window after Commit, the widget is force-hidden so
	// it never gets stuck in "processing".
	var pendingFinal atomic.Bool

	// maxRecTimer force-stops a recording that runs past the configured limit
	// (e.g. a stuck toggle).
	var maxRecMu sync.Mutex
	var maxRecTimer *time.Timer
	stopMaxRecTimer := func() {
		maxRecMu.Lock()
		if maxRecTimer != nil {
			maxRecTimer.Stop()
			maxRecTimer = nil
		}
		maxRecMu.Unlock()
	}

	// Pipeline (Result -> LLM -> Paste)
	go func() {
		liveWriter := paste.NewLiveWriter()
		var pasteMu sync.Mutex
		var lastLiveText string
		var lastLiveAt time.Time

		for text := range resultChan {
			if text.Final {
				pendingFinal.Store(false)
			}
			if strings.TrimSpace(text.Text) == "" {
				if text.Final {
					widget.Hide()
					setTrayRecording(false)
				}
				continue
			}

			log.Printf("STT Result: %s\n", text.Text)

			if text.Live && !text.Final {
				pasteMu.Lock()
				if text.Text != lastLiveText && time.Since(lastLiveAt) >= 250*time.Millisecond {
					liveWriter.Replace(text.Text)
					lastLiveText = text.Text
					lastLiveAt = time.Now()
				}
				pasteMu.Unlock()
				continue
			}

			finalText := text.Text
			c := cfgHolder.Get()
			if text.Final && !c.DisableLLM {
				llmStarted := time.Now()
				llmResult, err := llm.ProcessText(ctx, c.OpenAIKey, c.Prompt, text.Text)
				if err != nil {
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

			pasteMu.Lock()
			setTrayRecording(false)
			targetMu.Lock()
			tw := targetWindow
			targetMu.Unlock()
			if !focus.RestoreAndWait(tw, 600*time.Millisecond) {
				log.Printf("Focus restore timed out, pasting into current window")
			}
			time.Sleep(50 * time.Millisecond)
			if text.Live {
				liveWriter.ReplaceFinal(finalText)
				lastLiveText = finalText
				lastLiveAt = time.Time{}
				liveWriter.Forget()
				lastLiveText = ""
			} else {
				paste.PasteTextSmart(finalText)
			}
			// Flash a checkmark so the user sees the text landed.
			widget.SetStatus("done")
			time.Sleep(450 * time.Millisecond)
			widget.Hide()
			pasteMu.Unlock()
		}
	}()

	processor := make(chan []byte, 10)
	go func() {
		for wavData := range processor {
			transcribeStarted := time.Now()
			if path, err := store.SaveRecording(wavData); err != nil {
				log.Printf("Failed to save recording backup: %v", err)
			} else {
				log.Printf("Recording saved: %s", path)
			}
			c := cfgHolder.Get()
			var apiKey string
			if c.Provider == "deepgram" {
				apiKey = c.DeepgramKey
			} else {
				apiKey = c.OpenAIKey
			}
			result, err := stt.Transcribe(ctx, c.Provider, c.Model, apiKey, c.Language, c.STTPrompt, wavData)
			if err != nil {
				log.Printf("STT Error after %s: %v", time.Since(transcribeStarted).Round(time.Millisecond), err)
				widget.Hide()
				setTrayRecording(false)
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
			resultChan <- transcriptResult{Text: result.Text, Final: true}
		}
	}()

	var rtSession *stt.RealtimeSTT
	var rtMu sync.Mutex

	// Listen for Global Hotkeys
	go hotkey.Listen(
		hotkey.Config{
			Mode:    cfg.HotkeyMode,
			Start:   cfg.HotkeyStart,
			Stop:    cfg.HotkeyStop,
			History: cfg.HotkeyHistory,
		},
		func() {
			setTrayRecording(true)
			targetMu.Lock()
			targetWindow = focus.Current()
			targetMu.Unlock()
			widget.Show("listening")
			maxRecMu.Lock()
			maxRecTimer = time.AfterFunc(time.Duration(cfg.MaxRecordSeconds)*time.Second, func() {
				log.Printf("Recording exceeded %ds limit, force-stopping", cfg.MaxRecordSeconds)
				hotkey.ForceStop()
			})
			maxRecMu.Unlock()
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
			stopMaxRecTimer()
			widget.SetStatus("processing")
			if bootSTTMode == "realtime" {
				rtMu.Lock()
				if rtSession != nil {
					audio.StopRecording()
					if err := rtSession.Commit(); err != nil {
						log.Printf("Realtime commit error: %v", err)
						widget.Hide()
						setTrayRecording(false)
					} else {
						// Watchdog: never leave the widget stuck in
						// "processing" if the final transcript never arrives.
						pendingFinal.Store(true)
						time.AfterFunc(12*time.Second, func() {
							if pendingFinal.CompareAndSwap(true, false) {
								log.Printf("Realtime final transcript timed out")
								widget.Hide()
								setTrayRecording(false)
							}
						})
					}
					sessionToClose := rtSession
					go func() {
						time.Sleep(15 * time.Second)
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
		func() {
			fmt.Println()
			stopMaxRecTimer()
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
		func() {
			records, err := database.GetRecentHistory(context.Background(), 8)
			if err != nil {
				log.Printf("History query error: %v", err)
				return
			}
			items := make([]history.Item, 0, len(records))
			for _, r := range records {
				items = append(items, history.Item{Time: r.Time, Text: r.Text})
			}
			history.Toggle(items)
		},
	)
}

// runSupervisor relaunches the binary as a child with WISPWIND_CHILD=1 and
// its stderr piped to logs/crash.log, so Go panic traces are captured even in
// a -H=windowsgui build. Clean exits stop the loop; crashes restart the app
// (up to 5 times per 10 minutes).
func runSupervisor() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	logsDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	crashPath := filepath.Join(logsDir, "crash.log")

	var restarts []time.Time
	for {
		f, ferr := os.OpenFile(crashPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		cmd := exec.Command(exe)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "WISPWIND_CHILD=1")
		if ferr == nil {
			fmt.Fprintf(f, "\n--- child start %s ---\n", time.Now().Format("2006-01-02 15:04:05"))
			cmd.Stderr = f
			cmd.Stdout = f
		}
		runErr := cmd.Run()
		if ferr == nil {
			if runErr != nil {
				fmt.Fprintf(f, "--- child crashed %s: %v ---\n", time.Now().Format("2006-01-02 15:04:05"), runErr)
			}
			f.Close()
		}
		if runErr == nil {
			return // clean quit
		}
		now := time.Now()
		fresh := restarts[:0]
		for _, t := range restarts {
			if now.Sub(t) < 10*time.Minute {
				fresh = append(fresh, t)
			}
		}
		restarts = append(fresh, now)
		if len(restarts) > 5 {
			return // crash loop; give up
		}
		time.Sleep(2 * time.Second)
	}
}

func onExit() {
	// systray.Run returns after this, letting main's defers (DB close,
	// portaudio terminate, log file close) run normally.
	log.Println("Exiting application...")
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
	systray.SetIcon(trayicon.StatusIcon(recording))
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

func openPath(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
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
	return "Enabled (gpt-4o-mini)"
}
