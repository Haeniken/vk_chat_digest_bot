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
)

type YandexARTClient struct {
	cfg        config.ImageConfig
	httpClient *http.Client
}

func NewYandexARTClient(cfg config.ImageConfig, httpClient *http.Client) *YandexARTClient {
	return &YandexARTClient{cfg: cfg, httpClient: httpClient}
}

func (c *YandexARTClient) GenerateSummaryImage(ctx context.Context, summaryText string) ([]byte, error) {
	if !c.cfg.Enabled {
		return nil, nil
	}

	prompt := buildImagePrompt(summaryText, c.cfg.PromptMaxChars)
	operationID, err := c.start(ctx, prompt)
	if err != nil {
		return nil, err
	}
	imageBytes, err := c.wait(ctx, operationID)
	if err != nil {
		return nil, err
	}
	return withDDDWatermark(imageBytes), nil
}

type startRequest struct {
	ModelURI          string            `json:"modelUri"`
	GenerationOptions generationOptions `json:"generationOptions"`
	Messages          []imageMessage    `json:"messages"`
}

type generationOptions struct {
	AspectRatio aspectRatio `json:"aspectRatio"`
}

type aspectRatio struct {
	WidthRatio  string `json:"widthRatio"`
	HeightRatio string `json:"heightRatio"`
}

type imageMessage struct {
	Text string `json:"text"`
}

type operationResponse struct {
	ID       string `json:"id"`
	Done     bool   `json:"done"`
	Response *struct {
		Image string `json:"image"`
	} `json:"response,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *YandexARTClient) start(ctx context.Context, prompt string) (string, error) {
	payload := startRequest{
		ModelURI: fmt.Sprintf("art://%s/%s/latest", c.cfg.FolderID, c.cfg.Model),
		GenerationOptions: generationOptions{AspectRatio: aspectRatio{
			WidthRatio:  fmt.Sprintf("%d", c.cfg.WidthRatio),
			HeightRatio: fmt.Sprintf("%d", c.cfg.HeightRatio),
		}},
		Messages: []imageMessage{{Text: prompt}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal image request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/foundationModels/v1/imageGenerationAsync", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create image request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("perform image request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read image response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("image request returned status %d: %s", response.StatusCode, responseSnippet(responseBody))
	}

	var parsed operationResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", fmt.Errorf("decode image response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("image generation error: %s", parsed.Error.Message)
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return "", fmt.Errorf("image generation returned empty operation id")
	}
	return parsed.ID, nil
}

func (c *YandexARTClient) wait(ctx context.Context, operationID string) ([]byte, error) {
	deadline := time.NewTimer(c.cfg.Timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		image, done, err := c.poll(ctx, operationID)
		if err != nil {
			return nil, err
		}
		if done {
			if len(image) == 0 {
				return nil, fmt.Errorf("image generation completed without image")
			}
			return image, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("image generation timed out after %s", c.cfg.Timeout)
		case <-ticker.C:
		}
	}
}

func (c *YandexARTClient) poll(ctx context.Context, operationID string) ([]byte, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://operation.api.cloud.yandex.net:443/operations/"+operationID, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create image poll request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("perform image poll request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, false, fmt.Errorf("read image poll response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("image poll returned status %d: %s", response.StatusCode, responseSnippet(responseBody))
	}

	var parsed operationResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, false, fmt.Errorf("decode image poll response: %w", err)
	}
	if parsed.Error != nil {
		return nil, false, fmt.Errorf("image generation error: %s", parsed.Error.Message)
	}
	if !parsed.Done {
		return nil, false, nil
	}
	if parsed.Response == nil || strings.TrimSpace(parsed.Response.Image) == "" {
		return nil, true, nil
	}
	imageBytes, err := base64.StdEncoding.DecodeString(parsed.Response.Image)
	if err != nil {
		return nil, true, fmt.Errorf("decode image bytes: %w", err)
	}
	return imageBytes, true, nil
}

func buildImagePrompt(imagePrompt string, maxChars int) string {
	imagePrompt = strings.Join(strings.Fields(imagePrompt), " ")
	if maxChars > 0 {
		runes := []rune(imagePrompt)
		if len(runes) > maxChars {
			imagePrompt = string(runes[:maxChars])
		}
	}
	return "Цветная нуарная обложка газетного дайджеста: одна цельная сцена, без коллажа, без панелей, без триптиха. " +
		"Графический роман, жёсткая тушевая линия, глубокие синие и бирюзовые тени, тёплые жёлто-оранжевые огни, красные акценты, драматичный передний план. " +
		"На изображении не должно быть никакого текста: без Daily Drama Digest, номеров выпуска, газетных шапок, заголовков, реплик, вывесок, логотипов, водяных знаков, букв, слов и любой читаемой типографики. Без реалистичных лиц конкретных людей. " +
		"Визуальная идея: " + imagePrompt
}

func responseSnippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	runes := []rune(text)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return text
}
