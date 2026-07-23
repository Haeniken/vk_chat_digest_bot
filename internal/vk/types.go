package vk

import (
	"encoding/json"
	"time"
)

type IncomingMessage struct {
	SourceMessageID              int64
	ConversationMessageID        int64
	ChatID                       int64
	PeerID                       int64
	SenderID                     int64
	Text                         string
	ReplyToSourceMessageID       int64
	ReplyToConversationMessageID int64
	ReplyToSenderID              int64
	ReplyToText                  string
	SentAt                       time.Time
	IsOutgoing                   bool
}

type MessageEvent struct {
	PeerID                int64
	UserID                int64
	EventID               string
	ConversationMessageID int64
	Payload               json.RawMessage
}

type longPollServerResponse struct {
	Response struct {
		Key    string `json:"key"`
		Server string `json:"server"`
		TS     string `json:"ts"`
	} `json:"response"`
	Error *vkAPIError `json:"error,omitempty"`
}

type longPollResponse struct {
	TS      string           `json:"ts"`
	Updates []longPollUpdate `json:"updates"`
	Failed  int              `json:"failed,omitempty"`
}

type longPollUpdate struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

type messageObject struct {
	ID                    int64               `json:"id"`
	ConversationMessageID int64               `json:"conversation_message_id"`
	PeerID                int64               `json:"peer_id"`
	FromID                int64               `json:"from_id"`
	Date                  int64               `json:"date"`
	Text                  string              `json:"text"`
	Out                   int                 `json:"out"`
	ReplyMessage          *replyMessageObject `json:"reply_message,omitempty"`
}

type replyMessageObject struct {
	ID                    int64  `json:"id"`
	ConversationMessageID int64  `json:"conversation_message_id"`
	FromID                int64  `json:"from_id"`
	Text                  string `json:"text"`
}

type sendMessageResponse struct {
	Response int64       `json:"response"`
	Error    *vkAPIError `json:"error,omitempty"`
}

type sendMessageEventAnswerResponse struct {
	Response int         `json:"response"`
	Error    *vkAPIError `json:"error,omitempty"`
}

type editMessageResponse struct {
	Response int         `json:"response"`
	Error    *vkAPIError `json:"error,omitempty"`
}

type deleteMessageResponse struct {
	Response json.RawMessage `json:"response"`
	Error    *vkAPIError     `json:"error,omitempty"`
}

type messageNewObject struct {
	Message messageObject `json:"message"`
}

type messageEventObject struct {
	UserID                int64           `json:"user_id"`
	PeerID                int64           `json:"peer_id"`
	EventID               string          `json:"event_id"`
	ConversationMessageID int64           `json:"conversation_message_id"`
	Payload               json.RawMessage `json:"payload"`
}

type messagesUploadServerResponse struct {
	Response struct {
		UploadURL string `json:"upload_url"`
	} `json:"response"`
	Error *vkAPIError `json:"error,omitempty"`
}

type photoUploadResponse struct {
	Server int    `json:"server"`
	Photo  string `json:"photo"`
	Hash   string `json:"hash"`
}

type saveMessagesPhotoResponse struct {
	Response []struct {
		ID        int64  `json:"id"`
		OwnerID   int64  `json:"owner_id"`
		AccessKey string `json:"access_key"`
	} `json:"response"`
	Error *vkAPIError `json:"error,omitempty"`
}

type usersGetResponse struct {
	Response []struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"response"`
	Error *vkAPIError `json:"error,omitempty"`
}

type groupsGetByIDResponse struct {
	Response struct {
		Groups []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"groups"`
	} `json:"response"`
	Error *vkAPIError `json:"error,omitempty"`
}

type serverTimeResponse struct {
	Response int64       `json:"response"`
	Error    *vkAPIError `json:"error,omitempty"`
}

type vkAPIError struct {
	Code    int    `json:"error_code"`
	Message string `json:"error_msg"`
}
