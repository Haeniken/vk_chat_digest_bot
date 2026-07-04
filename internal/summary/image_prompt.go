package summary

import (
	"context"
	"log/slog"
	"strings"

	"bot-summary-vk/internal/llm"
)

const imagePromptSystemPrompt = "Ты превращаешь summary переписки в безопасный prompt для генерации одной цельной газетной нуарной иллюстрации. " +
	"Выбери из summary одну самую конкретную и зрительно понятную сцену, которая явно связана с событиями выпуска. Не делай абстрактную метафору, коллаж, диптих, триптих, панели или несколько отдельных сцен. " +
	"Опиши один цельный кадр: один главный силуэт или объект на переднем плане, одно действие, один конфликт, 4-6 узнаваемых предметов или деталей прямо из summary. Зритель должен понять, о какой ситуации выпуск, даже без текста summary. Композиция: цветная нуарная газетная обложка или графический роман, крупный драматичный передний план, городская улица или редакционная сцена, толпа силуэтов на фоне, жёсткая тушевая линия, глубокие синие и бирюзовые тени, тёплые жёлто-оранжевые огни, красные акценты, высокая контрастность. " +
	"Не используй реальные имена, фамилии, ники, user_id, мат, прямые оскорбления, дискриминационные ярлыки и слова вроде петух или собака. Заменяй людей обезличенными силуэтами и символами. " +
	"Не используй темы наркотиков, опьянения, психиатрии, скорой помощи и медицинских диагнозов; заменяй их безопасными сюрреалистическими символами вроде масок, шляп, театрального дыма и карнавального хаоса. " +
	"На изображении не должно быть вообще никакого текста: без Daily Drama Digest, номеров выпуска, сюжетных заголовков, названия газеты, masthead, подписей, реплик, плакатов, вывесок, логотипов, водяных знаков, букв, слов и любой читаемой типографики. " +
	"Ответь только готовым prompt на русском, 1-2 предложения, до 620 символов, без markdown и пояснений."

const fallbackImagePrompt = "Цветная нуарная газетная обложка без текста: крупный силуэт в шляпе на переднем плане смотрит на ночную улицу с толпой теней, мокрым асфальтом, бирюзовыми тенями, тёплыми окнами и красными акцентами; в кадре один конфликт вокруг сломанной машины идей, без букв, слов, вывесок, заголовков и логотипов."

func (s *Service) buildSummaryImagePrompt(ctx context.Context, peerID int64, summaryText string) string {
	input := llm.GenerateSummaryInput{
		SystemPrompt:    imagePromptSystemPrompt,
		UserPrompt:      "На картинке не должно быть никакого текста, включая Daily Drama Digest, номер выпуска, заголовки, вывески, подписи и логотипы.\nSummary для превращения в визуальную сцену:\n" + trimRunes(summaryText, 1600),
		MaxOutputTokens: 220,
	}

	output, err := s.llmClient.GenerateSummary(ctx, input)
	if err != nil {
		s.logger.Warn("failed to generate summary image prompt, using fallback",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return fallbackImagePrompt
	}

	prompt := cleanImagePrompt(output.Text)
	if prompt == "" {
		return fallbackImagePrompt
	}
	return prompt
}

func cleanImagePrompt(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "`\"'«»“”")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimPrefix(text, "Prompt:")
	text = strings.TrimPrefix(text, "Промпт:")
	text = strings.TrimSpace(text)
	return trimRunes(text, 650)
}

func trimRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}
