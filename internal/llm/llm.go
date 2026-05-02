package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"wispwind/internal/usage"
)

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type Result struct {
	Text  string
	Usage usage.TokenUsage
}

func ProcessText(ctx context.Context, apiKey, prompt, text string) (Result, error) {
	var lastErr error
	// Retry logic
	for i := 0; i < 3; i++ {
		res, err := doProcessText(ctx, apiKey, prompt, text)
		if err == nil {
			return res, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(time.Second * time.Duration(i+1)):
		}
	}
	return Result{}, fmt.Errorf("llm failed after 3 attempts: %v", lastErr)
}

func doProcessText(ctx context.Context, apiKey, prompt, text string) (Result, error) {
	reqBody := chatRequest{
		Model: "gpt-4o-mini", // Fast and capable
		Messages: []message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: text},
		},
	}

	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

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

	var res chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Result{}, err
	}

	if len(res.Choices) == 0 {
		return Result{}, fmt.Errorf("no choices returned")
	}

	return Result{
		Text: res.Choices[0].Message.Content,
		Usage: usage.TokenUsage{
			Type:         "tokens",
			InputTokens:  res.Usage.PromptTokens,
			TextTokens:   res.Usage.PromptTokens,
			OutputTokens: res.Usage.CompletionTokens,
			TotalTokens:  res.Usage.TotalTokens,
		},
	}, nil
}
