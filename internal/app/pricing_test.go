package app

import "testing"

func TestLLMChatModelPrices(t *testing.T) {
	tests := []struct {
		model string
		want  llmTokenPrice
	}{
		{
			model: "gpt-5.3-chat-latest",
			want:  llmTokenPrice{InputPerMillion: 1.75, CachedInputPerMillion: 0.175, OutputPerMillion: 14.00},
		},
		{
			model: "gpt-5-chat-latest",
			want:  llmTokenPrice{InputPerMillion: 1.25, CachedInputPerMillion: 0.125, OutputPerMillion: 10.00},
		},
		{
			model: "chat-latest",
			want:  llmTokenPrice{InputPerMillion: 5.00, CachedInputPerMillion: 0.50, OutputPerMillion: 30.00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got, ok := llmPrice(tt.model)
			if !ok {
				t.Fatalf("llmPrice() did not recognize %s", tt.model)
			}
			if got != tt.want {
				t.Fatalf("llmPrice() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
