package explain

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic is the optional paid backend. It is never the default: `local` is,
// and with `local` nothing leaves the machine. Choosing this one turns
// --anonymize on by default, and docs/ai.md lists exactly what is sent.
type Anthropic struct {
	Model  string
	APIKey string
}

// DefaultModel is the current Claude model this project is tested against.
const DefaultModel = "claude-opus-5"

func (a Anthropic) Explain(ctx context.Context, req Request) (Explanation, error) {
	opts := []option.RequestOption{}
	if a.APIKey != "" {
		opts = append(opts, option.WithAPIKey(a.APIKey))
	}
	client := anthropic.NewClient(opts...)

	model := a.Model
	if model == "" {
		model = DefaultModel
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(Prompt(req))),
		},
	})
	if err != nil {
		return Explanation{}, fmt.Errorf("anthropic: %w", err)
	}
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			return parse(text.Text)
		}
	}
	return Explanation{}, fmt.Errorf("anthropic returned no text")
}
