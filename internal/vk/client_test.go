package vk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
)

func TestDeleteMessageResponseAcceptsVKResponseShapes(t *testing.T) {
	tests := []string{
		`{"response":{"123":1}}`,
		`{"response":[{"peer_id":2000000004,"conversation_message_id":123,"response":1}]}`,
	}

	for _, body := range tests {
		var response deleteMessageResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", body, err)
		}
		if len(response.Response) == 0 {
			t.Fatalf("json.Unmarshal(%s) returned an empty response", body)
		}
	}
}

func TestFindProgressConversationMessageID(t *testing.T) {
	messages := []messageObject{
		{ConversationMessageID: 10, Out: 0, Text: "progress"},
		{ConversationMessageID: 11, Out: 1, Text: "another"},
		{ConversationMessageID: 12, Out: 1, Text: "progress"},
	}

	if got := findProgressConversationMessageID(messages, "progress"); got != 12 {
		t.Fatalf("findProgressConversationMessageID() = %d, want 12", got)
	}
	if got := findProgressConversationMessageID(messages, "missing"); got != 0 {
		t.Fatalf("findProgressConversationMessageID() = %d, want 0", got)
	}
}

func TestIsDNSResolutionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "direct DNS error",
			err:  &net.DNSError{Err: "no such host", Name: "api.vk.com", IsNotFound: true},
			want: true,
		},
		{
			name: "wrapped URL DNS error",
			err: &url.Error{
				Op:  "Post",
				URL: "https://api.vk.com/method/utils.getServerTime",
				Err: &net.DNSError{Err: "no such host", Name: "api.vk.com", IsNotFound: true},
			},
			want: true,
		},
		{
			name: "deep wrapped DNS error",
			err:  fmt.Errorf("perform vk request: %w", &net.DNSError{Err: "no such host", Name: "api.vk.com", IsNotFound: true}),
			want: true,
		},
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "plain network error",
			err:  errors.New("connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDNSResolutionError(tt.err); got != tt.want {
				t.Fatalf("isDNSResolutionError() = %v, want %v", got, tt.want)
			}
		})
	}
}
