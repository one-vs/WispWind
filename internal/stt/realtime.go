package stt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type RealtimeResult struct {
	Text  string
	Final bool
}

type RealtimeSTT struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	onResult func(RealtimeResult)
	ctx      context.Context
	cancel   func()

	items       map[string]string
	order       []string
	audioChunks int
}

func NewRealtimeSTT(ctx context.Context, apiKey, model, language, prompt string, sampleRate int, onResult func(RealtimeResult)) (*RealtimeSTT, error) {
	if err := ValidateRealtimeSampleRate(sampleRate); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", model)
	header := http.Header{}
	header.Add("Authorization", "Bearer "+apiKey)

	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	rs := &RealtimeSTT{
		conn:     conn,
		onResult: onResult,
		ctx:      ctx,
		cancel:   cancel,
		items:    make(map[string]string),
	}

	transcription := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
	}
	if language != "" && language != "auto" {
		transcription["language"] = language
	}

	sessionUpdate := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"type": "transcription",
			"audio": map[string]interface{}{
				"input": map[string]interface{}{
					"format":        "pcm16",
					"transcription": transcription,
					"noise_reduction": map[string]interface{}{
						"type": "near_field",
					},
					"turn_detection": map[string]interface{}{
						"type":                "server_vad",
						"threshold":           0.8,
						"prefix_padding_ms":   300,
						"silence_duration_ms": 700,
					},
				},
			},
		},
	}
	if err := rs.sendJSON(sessionUpdate); err != nil {
		cancel()
		conn.Close()
		return nil, err
	}

	go rs.listen()

	return rs, nil
}

func (rs *RealtimeSTT) sendJSON(v interface{}) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.conn.WriteJSON(v)
}

func (rs *RealtimeSTT) PushAudio(pcm []int16) error {
	buf := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}

	if err := rs.sendJSON(map[string]interface{}{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(buf),
	}); err != nil {
		return err
	}

	rs.mu.Lock()
	rs.audioChunks++
	rs.mu.Unlock()
	return nil
}

func (rs *RealtimeSTT) Commit() error {
	committed, err := rs.CommitPending(1)
	if err != nil {
		return err
	}
	if !committed {
		return fmt.Errorf("cannot commit realtime audio: no audio chunks were sent")
	}
	return nil
}

func (rs *RealtimeSTT) CommitPending(minChunks int) (bool, error) {
	rs.mu.Lock()
	audioChunks := rs.audioChunks
	if audioChunks < minChunks {
		rs.mu.Unlock()
		return false, nil
	}
	rs.audioChunks = 0
	rs.mu.Unlock()
	return true, rs.sendJSON(map[string]interface{}{"type": "input_audio_buffer.commit"})
}

func (rs *RealtimeSTT) Close() {
	rs.cancel()
	rs.conn.Close()
}

func (rs *RealtimeSTT) listen() {
	for {
		select {
		case <-rs.ctx.Done():
			return
		default:
			_, message, err := rs.conn.ReadMessage()
			if err != nil {
				select {
				case <-rs.ctx.Done():
				default:
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			var event map[string]interface{}
			if err := json.Unmarshal(message, &event); err != nil {
				continue
			}

			rs.handleEvent(event)
		}
	}
}

func (rs *RealtimeSTT) handleEvent(event map[string]interface{}) {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "conversation.item.input_audio_transcription.delta":
		itemID, _ := event["item_id"].(string)
		delta, _ := event["delta"].(string)
		if itemID == "" || delta == "" {
			return
		}
		rs.updateItem(itemID, delta, false)
	case "conversation.item.input_audio_transcription.completed":
		itemID, _ := event["item_id"].(string)
		transcript, _ := event["transcript"].(string)
		if itemID == "" {
			return
		}
		rs.setItem(itemID, transcript, true)
	case "error":
		if apiErr, ok := event["error"].(map[string]interface{}); ok {
			if code, _ := apiErr["code"].(string); code == "input_audio_buffer_commit_empty" {
				return
			}
		}
		log.Printf("Realtime API Error: %v", event["error"])
	}
}

func (rs *RealtimeSTT) updateItem(itemID, delta string, final bool) {
	rs.mu.Lock()
	if _, ok := rs.items[itemID]; !ok {
		rs.order = append(rs.order, itemID)
	}
	rs.items[itemID] += delta
	text := rs.joinLocked()
	rs.mu.Unlock()

	rs.onResult(RealtimeResult{Text: text, Final: final})
}

func (rs *RealtimeSTT) setItem(itemID, transcript string, final bool) {
	rs.mu.Lock()
	if _, ok := rs.items[itemID]; !ok {
		rs.order = append(rs.order, itemID)
	}
	rs.items[itemID] = transcript
	text := rs.joinLocked()
	rs.mu.Unlock()

	rs.onResult(RealtimeResult{Text: text, Final: final})
}

func (rs *RealtimeSTT) joinLocked() string {
	text := ""
	for _, itemID := range rs.order {
		if rs.items[itemID] == "" {
			continue
		}
		if text != "" {
			text += " "
		}
		text += rs.items[itemID]
	}
	return text
}

func ValidateRealtimeSampleRate(sampleRate int) error {
	if sampleRate != 24000 {
		return fmt.Errorf("realtime transcription requires 24 kHz PCM, got %d Hz", sampleRate)
	}
	return nil
}
