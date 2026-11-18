package review

import (
	"context"
	"net/http"
)

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

// Ask sends p and returns the answer's text: POST {baseURL}/v1/messages with
// headers x-api-key, anthropic-version AnthropicVersion and content-type
// application/json, and a body of exactly the keys model (Model), max_tokens
// (MaxOutputTokens), system (p.System) and messages (one user message,
// p.User): no tools, no tool_choice, no stream. It returns an error wrapping
// ErrModel for a status other than 200, a body it cannot decode, a stop_reason
// other than end_turn, or content that is not text blocks alone; otherwise the
// text blocks' text, joined.
func (a *Anthropic) Ask(ctx context.Context, p Prompt) (string, error) {
	_, _ = ctx, p // the skeleton's stub (step 1)

	return "", errNotImplemented
}
