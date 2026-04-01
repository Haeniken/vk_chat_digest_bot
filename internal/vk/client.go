package vk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
	values := url.Values{}
	values.Set("peer_id", strconv.FormatInt(peerID, 10))
	values.Set("message", text)
	values.Set("random_id", strconv.Itoa(c.randomID()))
	if strings.TrimSpace(formatData) != "" {
		values.Set("format_data", formatData)
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

func (c *Client) randomID() int {
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
