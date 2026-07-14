package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bot-summary-vk/internal/config"
	"bot-summary-vk/internal/usage"
)

type OpenAIClient struct {
	cfg        config.ImageConfig
	httpClient *http.Client
}

func NewOpenAIClient(cfg config.ImageConfig, httpClient *http.Client) *OpenAIClient {
	return &OpenAIClient{cfg: cfg, httpClient: httpClient}
}

func (c *OpenAIClient) GenerateSummaryImage(ctx context.Context, summaryText string) ([]byte, usage.ImageGenerationUsage, error) {
	if !c.cfg.Enabled {
		return nil, usage.ImageGenerationUsage{}, nil
	}

	prompt := buildOpenAIPrompt(summaryText, c.cfg.PromptMaxChars)
	return c.generate(ctx, prompt)
}

type openAIImageRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	N            int    `json:"n,omitempty"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
}

type openAIImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Usage *struct {
		InputTokens        int `json:"input_tokens"`
		InputTokensDetails *struct {
			ImageTokens int `json:"image_tokens"`
			TextTokens  int `json:"text_tokens"`
		} `json:"input_tokens_details,omitempty"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) generate(ctx context.Context, prompt string) ([]byte, usage.ImageGenerationUsage, error) {
	startedAt := time.Now()
	payload := openAIImageRequest{
		Model:        c.cfg.Model,
		Prompt:       prompt,
		N:            1,
		Size:         fmt.Sprintf("%dx%d", c.cfg.Width, c.cfg.Height),
		Quality:      "low",
		OutputFormat: "jpeg",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("marshal openai image request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("create openai image request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("perform openai image request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("read openai image response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("openai image request returned status %d: %s", response.StatusCode, responseSnippet(responseBody))
	}

	var parsed openAIImageResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("decode openai image response: %w", err)
	}
	if parsed.Error != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("openai image generation failed: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || strings.TrimSpace(parsed.Data[0].B64JSON) == "" {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("openai image response has no base64 image")
	}
	imageBytes, err := base64.StdEncoding.DecodeString(parsed.Data[0].B64JSON)
	if err != nil {
		return nil, usage.ImageGenerationUsage{}, fmt.Errorf("decode openai image bytes: %w", err)
	}
	imageUsage := usage.ImageGenerationUsage{Provider: "openai", Model: c.cfg.Model, Duration: time.Since(startedAt)}
	if parsed.Usage != nil {
		imageUsage.InputTokens = parsed.Usage.InputTokens
		imageUsage.OutputTokens = parsed.Usage.OutputTokens
		if parsed.Usage.InputTokensDetails != nil {
			imageUsage.InputTextTokens = parsed.Usage.InputTokensDetails.TextTokens
			imageUsage.InputImageTokens = parsed.Usage.InputTokensDetails.ImageTokens
		}
		if imageUsage.InputTextTokens == 0 && imageUsage.InputImageTokens == 0 {
			imageUsage.InputTextTokens = parsed.Usage.InputTokens
		}
	}
	return withDDDWatermark(imageBytes), imageUsage, nil
}

func buildOpenAIPrompt(imagePrompt string, maxChars int) string {
	imagePrompt = strings.Join(strings.Fields(imagePrompt), " ")
	if maxChars > 0 {
		runes := []rune(imagePrompt)
		if len(runes) > maxChars {
			imagePrompt = string(runes[:maxChars])
		}
	}
	return "Depict this exact visual idea as the central subject; do not replace it with a generic noir detective, newspaper cover, city crowd, random hat, or unrelated street scene. " +
		"Visual idea: " + imagePrompt + " " +
		"Make the concrete objects and actions from the visual idea immediately visible and dominant in the image. One continuous cinematic scene, not a collage and not panels. " +
		"Style: color noir graphic novel illustration, bold black ink outlines, sharp high-contrast shadows, expressive brushwork. " +
		"Lighting: deep teal and midnight-blue shadows, warm yellow-orange practical lights, selective red accents, rim lighting, hard dramatic shadows. " +
		"Composition: clear central subject, close foreground detail, layered depth, tense public-drama atmosphere. " +
		"No text anywhere in the generated image: no masthead, no headlines, no captions, no signs, no speech bubbles, no logos, no letters and no readable typography."
}
