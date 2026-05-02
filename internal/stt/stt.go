package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"wispwind/internal/usage"
)

type Result struct {
	Text  string
	Usage usage.TokenUsage
}

type sttResponseOpenAI struct {
	Text  string      `json:"text"`
	Usage openAIUsage `json:"usage"`
}

type openAIUsage struct {
	Type              string `json:"type"`
	InputTokens       int    `json:"input_tokens"`
	InputTokenDetails struct {
		TextTokens  int `json:"text_tokens"`
		AudioTokens int `json:"audio_tokens"`
	} `json:"input_token_details"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type deepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

func Transcribe(ctx context.Context, provider, model, apiKey, language, prompt string, wavData []byte) (Result, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		var result Result
		var err error
		if provider == "deepgram" {
			result, err = doTranscribeDeepgram(ctx, model, apiKey, language, wavData)
		} else {
			result, err = doTranscribeOpenAI(ctx, model, apiKey, language, prompt, wavData)
		}

		if err == nil {
			return result, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(time.Second * time.Duration(i+1)):
		}
	}
	return Result{}, fmt.Errorf("stt failed after 3 attempts: %v", lastErr)
}

func doTranscribeDeepgram(ctx context.Context, model, apiKey, language string, wavData []byte) (Result, error) {
	values := url.Values{}
	values.Set("smart_format", "true")
	if language == "" || language == "auto" {
		values.Set("detect_language", "true")
	} else {
		values.Set("language", language)
	}
	values.Set("model", model)
	reqURL := "https://api.deepgram.com/v1/listen?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(wavData))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	req.Header.Set("Content-Type", "audio/wav")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Result{}, fmt.Errorf("deepgram api error %d: %s", resp.StatusCode, string(b))
	}

	var res deepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Result{}, err
	}

	if len(res.Results.Channels) > 0 && len(res.Results.Channels[0].Alternatives) > 0 {
		return Result{Text: res.Results.Channels[0].Alternatives[0].Transcript}, nil
	}
	return Result{}, nil
}

func doTranscribeOpenAI(ctx context.Context, model, apiKey, language, prompt string, wavData []byte) (Result, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return Result{}, err
	}
	part.Write(wavData)
	writer.WriteField("model", model)
	writer.WriteField("response_format", "json")
	// Add temperature 0 for more deterministic transcription without hallucinations
	writer.WriteField("temperature", "0.0")
	if language != "" && language != "auto" {
		writer.WriteField("language", language)
	}
	if prompt != "" {
		writer.WriteField("prompt", prompt)
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Result{}, fmt.Errorf("api error %d: %s", resp.StatusCode, string(b))
	}

	var res sttResponseOpenAI
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Result{}, err
	}

	return Result{
		Text: res.Text,
		Usage: usage.TokenUsage{
			Type:         res.Usage.Type,
			InputTokens:  res.Usage.InputTokens,
			TextTokens:   res.Usage.InputTokenDetails.TextTokens,
			AudioTokens:  res.Usage.InputTokenDetails.AudioTokens,
			OutputTokens: res.Usage.OutputTokens,
			TotalTokens:  res.Usage.TotalTokens,
		},
	}, nil
}
