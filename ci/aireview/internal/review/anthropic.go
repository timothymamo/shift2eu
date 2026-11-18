package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxModelAnswerBytes caps the model API's answer the client reads: an answer
// of MaxOutputTokens is far below it.
const maxModelAnswerBytes = 4 << 20

// Anthropic is the client of the Anthropic Messages API. It never puts the key
// in an error.
type Anthropic struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAnthropic returns a client of the API at baseURL (https://api.anthropic.com
// in production, an httptest server's URL in tests) with apiKey. client carries
// the timeout.
func NewAnthropic(baseURL, apiKey string, client *http.Client) *Anthropic {
	return &Anthropic{baseURL: baseURL, apiKey: apiKey, client: client}
}

// messagesRequest is the Messages API's request: exactly these four keys, so no
// tools, no tool_choice and no stream.
type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesResponse is what Ask reads of the Messages API's answer.
type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// Ask sends p and returns the answer's text: POST {baseURL}/v1/messages with
// headers x-api-key, anthropic-version AnthropicVersion and content-type
// application/json, and a body of exactly the keys model (Model), max_tokens
// (MaxOutputTokens), system (p.System) and messages (one user message,
// p.User): no tools, no tool_choice, no stream. It returns an error wrapping
// ErrModel for a status other than 200, a body it cannot decode, a stop_reason
// other than end_turn, or content that is not text blocks alone; otherwise the
// text blocks' text, joined.
func (a *Anthropic) Ask(ctx context.Context, p Prompt) (string, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     Model,
		MaxTokens: MaxOutputTokens,
		System:    p.System,
		Messages:  []message{{Role: "user", Content: p.User}},
	})
	if err != nil {
		return "", fmt.Errorf("%w: encode the request: %w", ErrModel, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: build the request: %w", ErrModel, err)
	}

	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", AnthropicVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: POST /v1/messages: %w", ErrModel, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: POST /v1/messages: status %d", ErrModel, resp.StatusCode)
	}

	var answer messagesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelAnswerBytes)).Decode(&answer); err != nil {
		return "", fmt.Errorf("%w: decode the answer: %w", ErrModel, err)
	}

	if answer.StopReason != "end_turn" {
		return "", fmt.Errorf("%w: the model did not end its turn (stop_reason %q)", ErrModel, answer.StopReason)
	}

	if len(answer.Content) == 0 {
		return "", fmt.Errorf("%w: the answer holds no text", ErrModel)
	}

	var text strings.Builder

	for _, block := range answer.Content {
		if block.Type != "text" {
			return "", fmt.Errorf("%w: the answer holds a content block of type %q, not text", ErrModel, block.Type)
		}

		text.WriteString(block.Text)
	}

	return text.String(), nil
}
