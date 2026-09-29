package main

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

type LLMClient struct {
	client *openai.Client
	config *Config
}

func NewLLMClient(cfg *Config) *LLMClient {
	cConfig := openai.DefaultConfig(cfg.OpenAIKey)
	if cfg.OpenAIBaseURL != "" {
		cConfig.BaseURL = cfg.OpenAIBaseURL
	}
	return &LLMClient{
		client: openai.NewClientWithConfig(cConfig),
		config: cfg,
	}
}

func (l *LLMClient) Summarize(text string) string {
	return l.summarize(text, l.config.SummaryPrompt, defaultSummaryPrompt, 5000, openai.GPT3Dot5Turbo)
}

const defaultPaperPrompt = "You are summarizing a research paper for someone building an AI-assisted CAD design tool. In under 200 words, using bullet points, state: the problem, the method (inputs, outputs, and representation, e.g. CadQuery/OpenSCAD code, B-rep, mesh, sketch-extrude sequences), the datasets/benchmarks and headline results, and limitations. Then add one line, 'Relevance:', on what a text/image-to-CAD product could take from it. Ignore references and acknowledgements."

// SummarizePaper summarizes the markdown of a full paper (e.g. from MinerU).
func (l *LLMClient) SummarizePaper(markdown string) string {
	// Full papers are long: send far more than an article, and default to a larger-context model.
	return l.summarize(markdown, l.config.PaperSummaryPrompt, defaultPaperPrompt, 40000, openai.GPT4o)
}

const defaultSummaryPrompt = "Summarize the following news article concisely in plain language, covering the who, what, when, where, why, and how, and keep the summary under 150 words; use bullet points to make visual scanning easier:"

func (l *LLMClient) summarize(text, prompt, defaultPrompt string, maxCharacters int, defaultModel string) string {
	if l == nil || l.client == nil {
		return ""
	}

	const minCharactersToSummarize = 200

	if len(text) > maxCharacters {
		text = text[:maxCharacters]
	}

	// Don't summarize if the article is too short
	if len(text) < minCharactersToSummarize {
		fmt.Printf("  no summary: only %d characters of text (minimum %d)\n", len(text), minCharactersToSummarize)
		return ""
	}

	if prompt == "" {
		prompt = defaultPrompt
	}

	model := l.config.OpenAIModel
	if model == "" {
		model = defaultModel // TODO: change this
	}

	resp, err := l.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: prompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: text,
				},
			},
		},
	)

	if err != nil {
		fmt.Printf("  no summary: model %q returned an error: %v\n", model, err)
		return ""
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		fmt.Printf("  no summary: model %q returned an empty reply (sent %d characters)\n", model, len(text))
		return ""
	}

	fmt.Printf("  summarized %d characters with %q -> %d characters\n", len(text), model, len(resp.Choices[0].Message.Content))
	return resp.Choices[0].Message.Content
}

func (l *LLMClient) Analyze(articles []string, promptSuffix string) string {
	if l == nil || len(articles) == 0 {
		return ""
	}

	model := l.config.AnalystModel
	if model == "" {
		model = openai.GPT4o
	}

	prompt := fmt.Sprintf("%s%s\n\n%s", l.config.AnalystPrompt, promptSuffix, strings.Join(articles, "\n"))

	resp, err := l.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
		},
	)
	if err != nil {
		fmt.Printf("Analysis failed: %v\n", err)
		return ""
	}
	return resp.Choices[0].Message.Content
}
