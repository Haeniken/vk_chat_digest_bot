package usage

import "time"

type ImageGenerationUsage struct {
	Provider         string
	Model            string
	InputTokens      int
	InputTextTokens  int
	InputImageTokens int
	OutputTokens     int
	Duration         time.Duration
}
