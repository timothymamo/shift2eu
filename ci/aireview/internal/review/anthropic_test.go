package review_test

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// seenMessages is what the test server saw of the one request of an Ask call.
type seenMessages struct {
	method      string
	path        string
	apiKey      string
	version     string
	contentType string
	body        []byte
}

// answerText is the text of the reply the test server sends on success.
const answerText = "REVIEW-ANSWER"

// TestAnthropicAskSendsOneMessageWithoutTools pins the model request of ADR
// 0033: POST /v1/messages with x-api-key and anthropic-version, and a body of
// exactly model, max_tokens, system and messages. No tools, no tool_choice and
// no streaming: the model can do nothing but write the answer's text.
func TestAnthropicAskSendsOneMessageWithoutTools(t *testing.T) {
	t.Parallel()

	seen := make(chan seenMessages, 1)

	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read the request body: %v", err)
		}

		seen <- seenMessages{
			method:      r.Method,
			path:        r.URL.Path,
			apiKey:      r.Header.Get("x-api-key"),
			version:     r.Header.Get("anthropic-version"),
			contentType: r.Header.Get("Content-Type"),
			body:        body,
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"` + answerText + `"}],"stop_reason":"end_turn"}`))
	})

	client := review.NewAnthropic(srv.URL, testKey, srv.Client())

	want := review.Prompt{
		System: "the reviewer's instructions",
		User:   "<untrusted_pull_request>the pull request</untrusted_pull_request>",
	}

	got, err := client.Ask(t.Context(), want)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if got != answerText {
		t.Errorf("Ask = %q, want the reply's text %q", got, answerText)
	}

	var request seenMessages

	select {
	case request = <-seen:
	default:
		t.Fatal("Ask made no HTTP request")
	}

	if request.method != http.MethodPost {
		t.Errorf("the request's method is %s, want POST", request.method)
	}

	if request.path != "/v1/messages" {
		t.Errorf("the request's path is %s, want /v1/messages", request.path)
	}

	if request.apiKey != testKey {
		t.Errorf("the x-api-key header is %q, want the key %q", request.apiKey, testKey)
	}

	if request.version != review.AnthropicVersion {
		t.Errorf("the anthropic-version header is %q, want %q", request.version, review.AnthropicVersion)
	}

	if !strings.Contains(request.contentType, "application/json") {
		t.Errorf("the Content-Type header is %q, want application/json", request.contentType)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(request.body, &fields); err != nil {
		t.Fatalf("decode the request %q: %v", request.body, err)
	}

	wantFields := []string{"max_tokens", "messages", "model", "system"}
	if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(wantFields, keys) {
		t.Errorf("the request's top-level keys are %v, want exactly %v: no tools, no tool_choice, no stream", keys, wantFields)
	}

	var body struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatalf("decode the request %q: %v", request.body, err)
	}

	if body.Model != review.Model {
		t.Errorf("model = %q, want %q", body.Model, review.Model)
	}

	if body.MaxTokens != review.MaxOutputTokens {
		t.Errorf("max_tokens = %d, want %d", body.MaxTokens, review.MaxOutputTokens)
	}

	if body.System != want.System {
		t.Errorf("system = %q, want the prompt's system %q", body.System, want.System)
	}

	if len(body.Messages) != 1 {
		t.Fatalf("messages holds %d messages, want 1", len(body.Messages))
	}

	if body.Messages[0].Role != "user" {
		t.Errorf("the message's role is %q, want user", body.Messages[0].Role)
	}

	if body.Messages[0].Content != want.User {
		t.Errorf("the message's content is %q, want the prompt's user %q", body.Messages[0].Content, want.User)
	}
}

// TestAnthropicAskFailsItsTurn pins what a run cannot go on from (ADR 0033): an
// error status, a body it cannot decode, a turn that did not end
// (stop_reason other than end_turn, a refusal, a cut-off answer, a tool call) or
// content that is not text alone fails the run with ErrModel, so nothing is
// posted from a partial or unintended answer.
func TestAnthropicAskFailsItsTurn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		reply  string
	}{
		{
			name:   "an error status",
			status: http.StatusInternalServerError,
			reply:  `{"type":"error","error":{"type":"api_error","message":"overloaded"}}`,
		},
		{
			name:   "a body that is not JSON",
			status: http.StatusOK,
			reply:  "not JSON at all",
		},
		{
			name:   "a cut-off answer",
			status: http.StatusOK,
			reply:  `{"content":[{"type":"text","text":"half an ans"}],"stop_reason":"max_tokens"}`,
		},
		{
			name:   "a tool call",
			status: http.StatusOK,
			reply:  `{"content":[{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}],"stop_reason":"tool_use"}`,
		},
		{
			name:   "a refusal",
			status: http.StatusOK,
			reply:  `{"content":[],"stop_reason":"refusal"}`,
		},
		{
			name:   "a content block that is not text",
			status: http.StatusOK,
			reply:  `{"content":[{"type":"image","source":{"type":"base64","data":""}}],"stop_reason":"end_turn"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := server(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.reply))
			})

			client := review.NewAnthropic(srv.URL, testKey, srv.Client())

			if _, err := client.Ask(t.Context(), review.Prompt{System: "s", User: "u"}); !errors.Is(err, review.ErrModel) {
				t.Fatalf("Ask = %v, want an error wrapping ErrModel", err)
			}
		})
	}
}
