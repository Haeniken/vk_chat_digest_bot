package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"bot-summary-vk/internal/config"
)

type CloudflareClient struct {
	cfg        config.ImageConfig
	httpClient *http.Client
}

func NewCloudflareClient(cfg config.ImageConfig, httpClient *http.Client) *CloudflareClient {
	return &CloudflareClient{cfg: cfg, httpClient: httpClient}
}

func (c *CloudflareClient) GenerateSummaryImage(ctx context.Context, summaryText string) ([]byte, error) {
	if !c.cfg.Enabled {
		return nil, nil
	}

	prompt := buildCloudflarePrompt(summaryText, c.cfg.PromptMaxChars)
	return c.generate(ctx, prompt)
}

type cloudflareResponse struct {
	Success bool `json:"success"`
	Result  struct {
		Image string `json:"image"`
	} `json:"result"`
	Errors []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *CloudflareClient) generate(ctx context.Context, prompt string) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", prompt); err != nil {
		return nil, fmt.Errorf("write cloudflare prompt field: %w", err)
	}
	if err := writer.WriteField("width", fmt.Sprintf("%d", c.cfg.Width)); err != nil {
		return nil, fmt.Errorf("write cloudflare width field: %w", err)
	}
	if err := writer.WriteField("height", fmt.Sprintf("%d", c.cfg.Height)); err != nil {
		return nil, fmt.Errorf("write cloudflare height field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close cloudflare multipart body: %w", err)
	}

	requestURL := fmt.Sprintf("%s/client/v4/accounts/%s/ai/run/%s", c.cfg.BaseURL, c.cfg.AccountID, c.cfg.Model)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &body)
	if err != nil {
		return nil, fmt.Errorf("create cloudflare image request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform cloudflare image request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read cloudflare image response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("cloudflare image request returned status %d: %s", response.StatusCode, responseSnippet(responseBody))
	}

	contentType := response.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "image/") {
		return withDDDWatermark(responseBody), nil
	}

	var parsed cloudflareResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode cloudflare image response: %w", err)
	}
	if !parsed.Success {
		return nil, fmt.Errorf("cloudflare image generation failed: %s", cloudflareErrors(parsed.Errors))
	}
	if strings.TrimSpace(parsed.Result.Image) == "" {
		return nil, fmt.Errorf("cloudflare image response has empty result.image")
	}
	imageBytes, err := base64.StdEncoding.DecodeString(parsed.Result.Image)
	if err != nil {
		return nil, fmt.Errorf("decode cloudflare image bytes: %w", err)
	}
	return withDDDWatermark(imageBytes), nil
}

func buildCloudflarePrompt(imagePrompt string, maxChars int) string {
	imagePrompt = strings.Join(strings.Fields(imagePrompt), " ")
	if maxChars > 0 {
		runes := []rune(imagePrompt)
		if len(runes) > maxChars {
			imagePrompt = string(runes[:maxChars])
		}
	}
	return "Single unified color noir editorial newspaper cover illustration. " +
		"Visual idea: " + imagePrompt + " " +
		"Create one continuous cinematic scene, not a collage and not separate panels. The composition should look like a dramatic front-page newspaper cover with a clear central subject, strong depth, and a tense public-drama atmosphere. " +
		"Style: graphic novel editorial illustration, bold black ink outlines, sharp high-contrast shadows, vintage noir newspaper mood, cinematic framing, expressive brushwork. " +
		"Lighting: deep teal and midnight-blue shadows, warm yellow-orange practical lights, selective red accents, smoky atmosphere, rim lighting, hard dramatic shadows. " +
		"Composition: dramatic foreground silhouette framing the main scene, clear focal point, layered urban-noir depth, suspenseful scandal atmosphere. " +
		"Text: no text anywhere in the image. Do not render Daily Drama Digest, issue numbers, newspaper mastheads, headlines, signs, captions, speech bubbles, logos, watermarks, letters, words, or any readable typography."
}

func cloudflareErrors(errors []struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	parts := make([]string, 0, len(errors))
	for _, err := range errors {
		parts = append(parts, fmt.Sprintf("%d: %s", err.Code, err.Message))
	}
	return strings.Join(parts, "; ")
}
