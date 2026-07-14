package vk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-summary-vk/internal/config"
)

const apiBaseURL = "https://api.vk.com/method"

type Client struct {
	httpClient *http.Client
	cfg        config.VKConfig
	cacheMu    sync.RWMutex
	nameCache  map[int64]string
}

func NewClient(httpClient *http.Client, cfg config.VKConfig) *Client {
	return &Client{httpClient: httpClient, cfg: cfg, nameCache: make(map[int64]string)}
}

func (c *Client) Publish(ctx context.Context, peerID int64, text string) error {
	return c.PublishFormatted(ctx, peerID, text, "")
}

func (c *Client) PublishFormatted(ctx context.Context, peerID int64, text string, formatData string) error {
	return c.PublishFormattedWithRandomID(ctx, peerID, text, formatData, 0)
}

func (c *Client) PublishFormattedWithRandomID(ctx context.Context, peerID int64, text string, formatData string, randomID int) error {
	return c.sendMessage(ctx, peerID, text, formatData, "", randomID)
}

func (c *Client) PublishFormattedWithImage(ctx context.Context, peerID int64, text string, formatData string, image []byte) error {
	return c.PublishFormattedWithImageRandomID(ctx, peerID, text, formatData, image, 0)
}

func (c *Client) PublishFormattedWithImageRandomID(ctx context.Context, peerID int64, text string, formatData string, image []byte, randomID int) error {
	attachment, err := c.uploadMessagePhoto(ctx, peerID, image)
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, peerID, text, formatData, attachment, randomID)
}

func (c *Client) sendMessage(ctx context.Context, peerID int64, text string, formatData string, attachment string, randomID int) error {
	values := url.Values{}
	values.Set("peer_id", strconv.FormatInt(peerID, 10))
	values.Set("message", text)
	values.Set("random_id", strconv.Itoa(c.randomID(randomID)))
	if strings.TrimSpace(formatData) != "" {
		values.Set("format_data", formatData)
	}
	if strings.TrimSpace(attachment) != "" {
		values.Set("attachment", attachment)
	}

	var response sendMessageResponse
	if err := c.callMethod(ctx, "messages.send", values, &response); err != nil {
		return fmt.Errorf("messages.send: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("vk api error %d: %s", response.Error.Code, response.Error.Message)
	}
	return nil
}

func (c *Client) uploadMessagePhoto(ctx context.Context, peerID int64, image []byte) (string, error) {
	if len(image) == 0 {
		return "", fmt.Errorf("empty image")
	}

	candidates := imageUploadCandidates(image)
	var lastErr error
	for attempt, candidate := range candidates {
		uploadResponse, err := c.uploadMessagePhotoOnce(ctx, peerID, candidate.image, candidate.variant)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d/%d, %s, bytes=%d: %w", attempt+1, len(candidates), candidate.variant.description(), len(candidate.image), err)
			continue
		}

		attachment, err := c.saveMessagePhoto(ctx, uploadResponse)
		if err != nil {
			return "", err
		}
		return attachment, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("photo upload did not run")
}

type imageUploadCandidate struct {
	image   []byte
	variant imageUploadVariant
}

type imageUploadVariant struct {
	filename    string
	contentType string
}

func (v imageUploadVariant) description() string {
	if v.contentType == "" {
		return fmt.Sprintf("filename=%s content_type=<none>", v.filename)
	}
	return fmt.Sprintf("filename=%s content_type=%s", v.filename, v.contentType)
}

func imageUploadVariants(image []byte) []imageUploadVariant {
	contentType := http.DetectContentType(image)
	extension := imageExtension(contentType)
	variants := []imageUploadVariant{{filename: "summary" + extension, contentType: contentType}}
	if contentType != "image/jpeg" {
		variants = append(variants, imageUploadVariant{filename: "summary.jpg", contentType: "image/jpeg"})
	}
	variants = append(variants, imageUploadVariant{filename: "summary.jpg"})
	return variants
}

func imageUploadCandidates(image []byte) []imageUploadCandidate {
	candidates := make([]imageUploadCandidate, 0, 6)
	for _, variant := range imageUploadVariants(image) {
		candidates = append(candidates, imageUploadCandidate{image: image, variant: variant})
	}

	reencoded, err := reencodeJPEG(image)
	if err == nil && len(reencoded) > 0 && !bytes.Equal(reencoded, image) {
		for _, variant := range imageUploadVariants(reencoded) {
			candidates = append(candidates, imageUploadCandidate{image: reencoded, variant: variant})
		}
	}
	return candidates
}

func reencodeJPEG(imageBytes []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func imageExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func (c *Client) uploadMessagePhotoOnce(ctx context.Context, peerID int64, image []byte, variant imageUploadVariant) (photoUploadResponse, error) {
	values := url.Values{}
	values.Set("peer_id", strconv.FormatInt(peerID, 10))

	var serverResponse messagesUploadServerResponse
	if err := c.callMethod(ctx, "photos.getMessagesUploadServer", values, &serverResponse); err != nil {
		return photoUploadResponse{}, fmt.Errorf("photos.getMessagesUploadServer: %w", err)
	}
	if serverResponse.Error != nil {
		return photoUploadResponse{}, fmt.Errorf("vk api error %d: %s", serverResponse.Error.Code, serverResponse.Error.Message)
	}
	if strings.TrimSpace(serverResponse.Response.UploadURL) == "" {
		return photoUploadResponse{}, fmt.Errorf("photos.getMessagesUploadServer returned empty upload_url")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	var part io.Writer
	var err error
	if variant.contentType == "" {
		part, err = writer.CreateFormFile("photo", variant.filename)
	} else {
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="photo"; filename="%s"`, variant.filename))
		partHeader.Set("Content-Type", variant.contentType)
		part, err = writer.CreatePart(partHeader)
	}
	if err != nil {
		return photoUploadResponse{}, fmt.Errorf("create photo form field: %w", err)
	}
	if _, err := part.Write(image); err != nil {
		return photoUploadResponse{}, fmt.Errorf("write photo form field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return photoUploadResponse{}, fmt.Errorf("close photo multipart body: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serverResponse.Response.UploadURL, &body)
	if err != nil {
		return photoUploadResponse{}, fmt.Errorf("build photo upload request: %w", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := c.httpClient.Do(request)
	if err != nil {
		return photoUploadResponse{}, fmt.Errorf("perform photo upload: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return photoUploadResponse{}, fmt.Errorf("read photo upload response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return photoUploadResponse{}, fmt.Errorf("photo upload returned status %d for %d bytes: %s", response.StatusCode, len(image), responseSnippet(responseBody))
	}

	var uploadResponse photoUploadResponse
	if err := json.Unmarshal(responseBody, &uploadResponse); err != nil {
		return photoUploadResponse{}, fmt.Errorf("decode photo upload response: %w", err)
	}
	if uploadResponse.Server == 0 || strings.TrimSpace(uploadResponse.Photo) == "" || strings.TrimSpace(uploadResponse.Hash) == "" {
		return photoUploadResponse{}, fmt.Errorf("photo upload returned incomplete response for %d bytes: %s", len(image), responseSnippet(responseBody))
	}
	return uploadResponse, nil
}

func (c *Client) saveMessagePhoto(ctx context.Context, uploadResponse photoUploadResponse) (string, error) {
	saveValues := url.Values{}
	saveValues.Set("server", strconv.Itoa(uploadResponse.Server))
	saveValues.Set("photo", uploadResponse.Photo)
	saveValues.Set("hash", uploadResponse.Hash)

	var saveResponse saveMessagesPhotoResponse
	if err := c.callMethod(ctx, "photos.saveMessagesPhoto", saveValues, &saveResponse); err != nil {
		return "", fmt.Errorf("photos.saveMessagesPhoto: %w", err)
	}
	if saveResponse.Error != nil {
		return "", fmt.Errorf("vk api error %d: %s", saveResponse.Error.Code, saveResponse.Error.Message)
	}
	if len(saveResponse.Response) == 0 {
		return "", fmt.Errorf("photos.saveMessagesPhoto returned empty response")
	}
	photo := saveResponse.Response[0]
	attachment := fmt.Sprintf("photo%d_%d", photo.OwnerID, photo.ID)
	if strings.TrimSpace(photo.AccessKey) != "" {
		attachment += "_" + photo.AccessKey
	}
	return attachment, nil
}

func (c *Client) GetLongPollServer(ctx context.Context) (string, string, string, error) {
	values := url.Values{}
	values.Set("group_id", strconv.FormatInt(c.cfg.GroupID, 10))

	var response longPollServerResponse
	if err := c.callMethod(ctx, "groups.getLongPollServer", values, &response); err != nil {
		return "", "", "", fmt.Errorf("groups.getLongPollServer: %w", err)
	}
	if response.Error != nil {
		return "", "", "", fmt.Errorf("vk api error %d: %s", response.Error.Code, response.Error.Message)
	}
	return response.Response.Server, response.Response.Key, response.Response.TS, nil
}

func (c *Client) ResolveSenderName(ctx context.Context, senderID int64) (string, error) {
	if senderID == 0 {
		return "", nil
	}

	c.cacheMu.RLock()
	if cached := c.nameCache[senderID]; cached != "" {
		c.cacheMu.RUnlock()
		return cached, nil
	}
	c.cacheMu.RUnlock()

	var (
		name string
		err  error
	)
	if senderID > 0 {
		name, err = c.resolveUserName(ctx, senderID)
	} else {
		name, err = c.resolveGroupName(ctx, -senderID)
	}
	if err != nil {
		return "", err
	}

	c.cacheMu.Lock()
	c.nameCache[senderID] = name
	c.cacheMu.Unlock()
	return name, nil
}

func (c *Client) resolveUserName(ctx context.Context, userID int64) (string, error) {
	values := url.Values{}
	values.Set("user_ids", strconv.FormatInt(userID, 10))

	var response usersGetResponse
	if err := c.callMethod(ctx, "users.get", values, &response); err != nil {
		return "", fmt.Errorf("users.get: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("vk api error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Response) == 0 {
		return "", fmt.Errorf("users.get returned empty response")
	}
	fullName := strings.TrimSpace(response.Response[0].FirstName + " " + response.Response[0].LastName)
	if fullName == "" {
		return "", fmt.Errorf("users.get returned empty name")
	}
	return fullName, nil
}

func (c *Client) resolveGroupName(ctx context.Context, groupID int64) (string, error) {
	values := url.Values{}
	values.Set("group_ids", strconv.FormatInt(groupID, 10))

	var response groupsGetByIDResponse
	if err := c.callMethod(ctx, "groups.getById", values, &response); err != nil {
		return "", fmt.Errorf("groups.getById: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("vk api error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Response.Groups) == 0 {
		return "", fmt.Errorf("groups.getById returned empty response")
	}
	name := strings.TrimSpace(response.Response.Groups[0].Name)
	if name == "" {
		return "", fmt.Errorf("groups.getById returned empty name")
	}
	return name, nil
}

func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	startedAt := time.Now()

	var response serverTimeResponse
	if err := c.callMethod(ctx, "utils.getServerTime", url.Values{}, &response); err != nil {
		return 0, fmt.Errorf("utils.getServerTime: %w", err)
	}
	if response.Error != nil {
		return 0, fmt.Errorf("vk api error %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.Response == 0 {
		return 0, fmt.Errorf("utils.getServerTime returned empty response")
	}
	return time.Since(startedAt), nil
}

func (c *Client) callMethod(ctx context.Context, method string, values url.Values, target any) error {
	requestURL := apiBaseURL + "/" + method
	values = cloneValues(values)
	values.Set("access_token", c.cfg.AccessToken)
	values.Set("v", c.cfg.APIVersion)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("build vk request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform vk request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read vk response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("vk returned status %d", response.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode vk response: %w", err)
	}
	return nil
}

func (c *Client) randomID(override int) int {
	if override != 0 {
		return override
	}
	if c.cfg.SendRandomID != 0 {
		return c.cfg.SendRandomID
	}
	return rand.New(rand.NewSource(time.Now().UnixNano())).Int()
}

func cloneValues(values url.Values) url.Values {
	copied := url.Values{}
	for key, value := range values {
		copied[key] = append([]string(nil), value...)
	}
	return copied
}

func responseSnippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	runes := []rune(text)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return text
}
