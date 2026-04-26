package vk

import "time"

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
	Type   string `json:"type"`
	Object struct {
		Message messageObject `json:"message"`
	} `json:"object"`
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

type vkAPIError struct {
	Code    int    `json:"error_code"`
	Message string `json:"error_msg"`
}
