package explain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Local talks to an OpenAI-compatible endpoint on this machine — mlx_lm.server,
// LM Studio or Ollama. It is the default backend, and with it nothing leaves the
// machine, which is why --anonymize defaults off for it.
type Local struct {
	URL    string
	Model  string
	Client *http.Client
}

func (l Local) Explain(ctx context.Context, req Request) (Explanation, error) {
	body := map[string]any{
		"model":       l.Model,
		"temperature": 0,
		"seed":        0,
		"max_tokens":  512,
		// Thinking is off here: latency is the user's cost at a prompt. It is on
		// for build-time generation and for cluster-bench, where quality is the
		// point and nobody is waiting.
		"messages": []map[string]string{{"role": "user", "content": Prompt(req)}},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "explanation", "schema": jsonSchema()},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Explanation{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, l.URL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Explanation{}, err
	}
	httpReq.Header.Set("content-type", "application/json")

	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Explanation{}, fmt.Errorf("local model at %s: %w", l.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Explanation{}, fmt.Errorf("local model at %s returned %s", l.URL, resp.Status)
	}

	var decoded struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Explanation{}, err
	}
	if len(decoded.Choices) == 0 {
		return Explanation{}, fmt.Errorf("local model returned no choices")
	}
	return parse(decoded.Choices[0].Message.Content)
}

// jsonSchema is docs/schema/explain.v1.json in the shape an OpenAI-compatible
// endpoint wants. Validate() applies the same rules to whatever comes back, so a
// backend that ignores the schema still cannot reach a user.
func jsonSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"code", "lines", "next_command"},
		"properties": map[string]any{
			"code": map[string]any{"type": "string"},
			"lines": map[string]any{
				"type": "array", "minItems": 3, "maxItems": 3,
				"items": map[string]any{"type": "string", "maxLength": 160},
			},
			"next_command": map[string]any{"type": "string", "maxLength": 300},
		},
	}
}
