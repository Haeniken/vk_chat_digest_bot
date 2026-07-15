package vk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type MessageHandler func(context.Context, IncomingMessage) error
type MessageEventHandler func(context.Context, MessageEvent) error

type LongPollConsumer struct {
	client      *Client
	httpClient  *http.Client
	logger      *slog.Logger
	waitSeconds int
}

func NewLongPollConsumer(client *Client, httpClient *http.Client, waitSeconds int, logger *slog.Logger) *LongPollConsumer {
	return &LongPollConsumer{client: client, httpClient: httpClient, logger: logger, waitSeconds: waitSeconds}
}

func (c *LongPollConsumer) Run(ctx context.Context, messageHandler MessageHandler, eventHandler MessageEventHandler) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		server, key, ts, err := c.client.GetLongPollServer(ctx)
		if err != nil {
			c.logger.Error("failed to bootstrap vk long poll", slog.String("error", err.Error()))
			if err := sleepWithContext(ctx, 3*time.Second); err != nil {
				return err
			}
			continue
		}

		if err := c.consumeLoop(ctx, server, key, ts, messageHandler, eventHandler); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.logger.Error("vk long poll loop failed, reconnecting", slog.String("error", err.Error()))
			if err := sleepWithContext(ctx, 3*time.Second); err != nil {
				return err
			}
		}
	}
}

func (c *LongPollConsumer) consumeLoop(ctx context.Context, server, key, ts string, messageHandler MessageHandler, eventHandler MessageEventHandler) error {
	for {
		response, err := c.poll(ctx, server, key, ts)
		if err != nil {
			return err
		}

		switch response.Failed {
		case 0:
			ts = response.TS
		case 1:
			ts = response.TS
			continue
		case 2, 3:
			return fmt.Errorf("long poll server expired with failed=%d", response.Failed)
		default:
			return fmt.Errorf("unexpected long poll failed=%d", response.Failed)
		}

		for _, update := range response.Updates {
			switch update.Type {
			case "message_new":
				raw, err := parseMessageNew(update.Object)
				if err != nil {
					c.logger.Warn("failed to decode vk message_new",
						slog.String("error", err.Error()),
					)
					continue
				}
				if isChatPeer(raw.PeerID) {
					c.logger.Info("vk long poll message_new received",
						slog.Int64("peer_id", raw.PeerID),
						slog.Int64("chat_id", chatIDFromPeer(raw.PeerID)),
						slog.Int64("from_id", raw.FromID),
						slog.Int64("message_id", raw.ID),
						slog.Int64("conversation_message_id", raw.ConversationMessageID),
						slog.Bool("is_outgoing", raw.Out == 1),
						slog.Int("text_len", len([]rune(raw.Text))),
						slog.String("text_preview", previewText(raw.Text, 80)),
					)
				}
				message := mapIncomingMessage(raw)
				if !isChatPeer(message.PeerID) {
					continue
				}
				if err := messageHandler(ctx, message); err != nil {
					return fmt.Errorf("handle incoming message: %w", err)
				}
			case "message_event":
				if eventHandler == nil {
					continue
				}
				event, err := parseMessageEvent(update.Object)
				if err != nil {
					c.logger.Warn("failed to decode vk message_event",
						slog.String("error", err.Error()),
					)
					continue
				}
				if isChatPeer(event.PeerID) {
					c.logger.Info("vk long poll message_event received",
						slog.Int64("peer_id", event.PeerID),
						slog.Int64("chat_id", chatIDFromPeer(event.PeerID)),
						slog.Int64("user_id", event.UserID),
						slog.Int64("conversation_message_id", event.ConversationMessageID),
					)
				}
				if !isChatPeer(event.PeerID) {
					continue
				}
				if err := eventHandler(ctx, event); err != nil {
					return fmt.Errorf("handle message event: %w", err)
				}
			default:
				continue
			}
		}
	}
}

func (c *LongPollConsumer) poll(ctx context.Context, server, key, ts string) (longPollResponse, error) {
	requestURL, err := url.Parse(server)
	if err != nil {
		return longPollResponse{}, fmt.Errorf("parse long poll server url: %w", err)
	}

	query := requestURL.Query()
	query.Set("act", "a_check")
	query.Set("key", key)
	query.Set("ts", ts)
	query.Set("wait", strconv.Itoa(c.waitSeconds))
	query.Set("mode", "10")
	query.Set("version", "3")
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return longPollResponse{}, fmt.Errorf("build long poll request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return longPollResponse{}, fmt.Errorf("perform long poll request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return longPollResponse{}, fmt.Errorf("read long poll response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return longPollResponse{}, fmt.Errorf("long poll returned status %d", response.StatusCode)
	}

	var parsed longPollResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return longPollResponse{}, fmt.Errorf("decode long poll response: %w", err)
	}
	return parsed, nil
}

func parseMessageNew(raw json.RawMessage) (messageObject, error) {
	var object messageNewObject
	if err := json.Unmarshal(raw, &object); err != nil {
		return messageObject{}, err
	}
	return object.Message, nil
}

func parseMessageEvent(raw json.RawMessage) (MessageEvent, error) {
	var object messageEventObject
	if err := json.Unmarshal(raw, &object); err != nil {
		return MessageEvent{}, err
	}
	return MessageEvent{
		PeerID:                object.PeerID,
		UserID:                object.UserID,
		EventID:               object.EventID,
		ConversationMessageID: object.ConversationMessageID,
		Payload:               object.Payload,
	}, nil
}

func mapIncomingMessage(message messageObject) IncomingMessage {
	sourceMessageID := message.ID
	if sourceMessageID == 0 {
		sourceMessageID = message.ConversationMessageID
	}
	incoming := IncomingMessage{
		SourceMessageID:       sourceMessageID,
		ConversationMessageID: message.ConversationMessageID,
		ChatID:                chatIDFromPeer(message.PeerID),
		PeerID:                message.PeerID,
		SenderID:              message.FromID,
		Text:                  message.Text,
		SentAt:                time.Unix(message.Date, 0).UTC(),
		IsOutgoing:            message.Out == 1,
	}
	if message.ReplyMessage != nil {
		replySourceMessageID := message.ReplyMessage.ID
		if replySourceMessageID == 0 {
			replySourceMessageID = message.ReplyMessage.ConversationMessageID
		}
		incoming.ReplyToSourceMessageID = replySourceMessageID
		incoming.ReplyToConversationMessageID = message.ReplyMessage.ConversationMessageID
		incoming.ReplyToSenderID = message.ReplyMessage.FromID
		incoming.ReplyToText = message.ReplyMessage.Text
	}
	return incoming
}

func chatIDFromPeer(peerID int64) int64 {
	const peerOffset int64 = 2_000_000_000
	if peerID <= peerOffset {
		return peerID
	}
	return peerID - peerOffset
}

func isChatPeer(peerID int64) bool {
	const peerOffset int64 = 2_000_000_000
	return peerID > peerOffset
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func previewText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
