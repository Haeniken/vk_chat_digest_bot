package summary

import (
	"context"
	"log/slog"
	"strings"

	"bot-summary-vk/internal/llm"
)

const imagePromptSystemPrompt = "Ты превращаешь summary переписки в безопасный prompt для генерации одной цельной цветной нуарной иллюстрации. " +
	"Главная цель: картинка должна быть узнаваемо про тот же выпуск, а не просто красивый общий нуар. Сначала найди в summary один самый зрительный эпизод: конкретное действие, место, конфликт и предметы. " +
	"Не выбирай абстрактную мораль, настроение, редакцию, газетную обложку, детектива в шляпе, толпу на улице или сломанную машину идей, если этого прямо нет в summary. Не делай коллаж, диптих, триптих, панели или несколько отдельных сцен. " +
	"Опиши один цельный кадр. Обязательно включи 4-7 конкретных визуальных деталей из summary: еду, напитки, технику, одежду, мебель, транспорт, животных-символы, жесты, погоду, город, комнату, чат на экране или другие предметы, если они есть в тексте. " +
	"Если в summary есть бытовая сцена, рисуй бытовую сцену; если спор про долги, вес, еду, поездку, картинки, колу, грибы, шляпы или чат, сделай именно это центральным объектом кадра. " +
	"Людей показывай обезличенными силуэтами или карикатурными фигурами, без реалистичных лиц конкретных людей. Не используй реальные имена, фамилии, ники, user_id, мат, прямые оскорбления, дискриминационные ярлыки и слова вроде петух или собака. Заменяй спорные роли безопасными визуальными символами. " +
	"Не используй темы наркотиков, опьянения, психиатрии, скорой помощи и медицинских диагнозов; заменяй их безопасными сюрреалистическими символами вроде масок, шляп, театрального тумана и карнавального хаоса. " +
	"На изображении не должно быть вообще никакого текста: без Daily Drama Digest, номеров выпуска, сюжетных заголовков, названия газеты, masthead, подписей, реплик, плакатов, вывесок, логотипов, водяных знаков, букв, слов и любой читаемой типографики. " +
	"Ответь только готовым prompt на русском. Формат: одно плотное предложение до 900 символов. Без markdown, списков и пояснений."

const fallbackImagePrompt = "Цветная нуарная иллюстрация без текста: крупный экран чата на столе, вокруг него разбросаны предметы бытовой ссоры, чашки, провода, телефон, красная лампа и тени спорящих силуэтов; кадр должен выглядеть как конкретная сцена переписки, без букв, слов, вывесок, заголовков и логотипов."

func (s *Service) buildSummaryImagePrompt(ctx context.Context, peerID int64, summaryText string) (string, llm.GenerateSummaryOutput) {
	input := llm.GenerateSummaryInput{
		SystemPrompt:    imagePromptSystemPrompt,
		UserPrompt:      "Сделай картинку максимально совпадающей с конкретными событиями summary. Не рисуй общий noir, детектива, газетную обложку, толпу или городскую улицу по умолчанию. Сначала опирайся на предметы и действия из текста. На картинке не должно быть никакого текста, включая Daily Drama Digest, номер выпуска, заголовки, вывески, подписи и логотипы.\n\nSummary для превращения в визуальную сцену:\n" + trimRunes(summaryText, 2400),
		MaxOutputTokens: 320,
	}

	imagePromptClient := s.imagePromptLLMClient
	if imagePromptClient == nil {
		imagePromptClient = s.llmClient
	}
	output, err := imagePromptClient.GenerateSummary(ctx, input)
	if err != nil {
		s.logger.Warn("failed to generate summary image prompt, using fallback",
			slog.Int64("peer_id", peerID),
			slog.String("error", err.Error()),
		)
		return fallbackImagePrompt, llm.GenerateSummaryOutput{}
	}

	prompt := cleanImagePrompt(output.Text)
	if prompt == "" {
		return fallbackImagePrompt, llm.GenerateSummaryOutput{}
	}
	s.logger.Info("summary image prompt generated",
		slog.Int64("peer_id", peerID),
		slog.Int("prompt_chars", len([]rune(prompt))),
		slog.String("prompt_preview", trimRunes(prompt, 320)),
	)
	return prompt, output
}

func cleanImagePrompt(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "`\"'«»“”")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimPrefix(text, "Prompt:")
	text = strings.TrimPrefix(text, "Промпт:")
	text = strings.TrimSpace(text)
	return trimRunes(text, 1000)
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
