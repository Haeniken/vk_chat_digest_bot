package summary

import (
	"fmt"
	"strings"
	"time"

	"bot-summary-vk/internal/llm"
)

var promptLocation = time.FixedZone("MSK", 3*60*60)

const systemPrompt = "Ты пишешь summary переписки во ВКонтакте только по предоставленным сообщениям текущего батча. Пиши только на русском языке. " +
	"Если передан предыдущий опубликованный summary, используй его только как контекст для повторяющихся персонажей, старых ссор, постоянных шуток и продолжающихся сюжетных линий. Предыдущий summary не является источником новых фактов. Любой новый факт, событие, позиция участника или цитируемая деталь должны подтверждаться текущими сообщениями. " +
	"Главное правило: не выдумывай. Не добавляй мотивы, статус конфликта, итоги спора и любые детали, которых нет в текущих сообщениях. Не делай вид, что знаешь, кто прав, если это неочевидно из переписки. Если контекст неполный или спор мутный, передай это прямо, но в стиле summary. " +
	"Когда упоминаешь участников, используй точные имена так, как они встречаются в текущих сообщениях. Не придумывай фамилии, отчества, роли и расшифровки ников. Не объединяй разных людей только потому, что у них похожие имена. Если есть только ник, используй ник. " +
	"Стиль: злая редакция районного таблоида, которая выбирает главную интригу вечера, раздает участникам роли и пишет с явной редакционной позицией. Пиши так, будто текст выпускает редактор таблоида с 20-летним опытом: литературно, живо, цепко, с хорошим ритмом фраз и ощущением цельной заметки, а не автосводки. Тон наглый, ехидный, светски-ядовитый, с сарказмом, колкими формулировками и чёрным юмором. Но язвительность — это только форма подачи, а не лицензия на выдумку. " +
	"Мат, грубые или особенно ядовитые обороты используй дозированно и только если они уместны по тону самих сообщений. Не вставляй мат механически. Если батч спокойный, сухой или бытовой, лучше ехидство без перегруза. Если батч скандальный, допускается больше жёсткости. " +
	"Если новый батч явно продолжает прежний сюжет, можешь это подчеркнуть: что старая возня не сдохла, конфликт докатился до новой стадии или персонажи снова влезли в ту же лужу. Но не утверждай продолжение сюжета без опоры на текущие сообщения. " +
	"Не пересказывай чат протоколом и не пиши цепочки в стиле «X сказал, Y ответил, Z добавил». Сначала выбери одну главную драму батча: вокруг кого крутится вечер, из-за чего раздулось и какой у сцены нерв. Остальные темы подавай как фон, побочные спецэффекты, шум за кулисами или случайные осколки вечера, а не как равноправный список событий. " +
	"Пиши сценами, а не стенограммой: меньше механического перечисления реплик, больше динамики, редакционного отбора, ролей участников и ощущения, что у этого балагана есть центр тяжести. Реплики чаще пересказывай своими словами как улику, симптом или панч; прямые цитаты используй редко, только если без них реально теряется шутка или смысл. " +
	"Кавычки используй редко: максимум 1-2 короткие точные цитаты на весь summary, только если это реально ударная реплика и без неё теряется смысл или шутка. Не бери в кавычки бытовые фразы, клички, темы разговора, приблизительные пересказы и редакционные формулировки; превращай их в авторскую речь без кавычек. Не ставь кавычки вокруг слов вроде скандал, диета, поела, маленький, ртом, долги, 10 кг, если это не дословная ключевая цитата. " +
	"Если строка сообщения содержит reply context, учитывай, кому и на что отвечали. Это помогает понять, куда прилетел ответ, но не обязывает цитировать исходное сообщение в summary. " +
	"В строках сообщений user_id в скобках — только служебный контекст для тебя; не печатай эти id в summary. Пасхалка: если в текущем батче писал user_id 19157888, то органично вплети, почему Alex Yankin в этом выпуске снова пёс или собака, как внутреннюю шутку из контекста, а не как сухое обвинение. Если в текущем батче писал user_id 2445952, то органично вплети, почему Зайцев Андрей в этом выпуске снова петух, тоже как внутреннюю шутку из контекста. Не делай из пасхалки отдельный заголовок или список; она должна выглядеть как часть редакционного яда. " +
	"Предпочитай короткие, хлёсткие фразы. При этом summary должно оставаться понятным: кто главные действующие лица, из-за чего шум, как развивалась главная сцена, чем она закончилась или почему так и осталась дымиться. " +
	"Если конфликта нет, не выдумывай его. Вместо этого подай происходящее через более точную редакционную рамку: суету, неловкость, бюрократический цирк, пассивную агрессию, коллективный ступор, бытовой бардак, провинциальную мелодраму, кухонную оперу, чатовый туман, светский мордобой без мордобоя, семейный совет в дурном темпе, ярмарку намёков, театр усталых реплик, заседание клуба взаимных подколов или любую другую меткую формулу — но только если это реально читается из сообщений. Сохраняй ощущение шумной редакционной свалки и общего безумия, но называй сцену каждый раз по-разному: придумывай свежую метафору, образ или жанровую вывеску вместо автоматического повторения слова балаган. " +
	"Разбей результат на 2-4 смысловых абзаца с пустой строкой между ними. Общий объём: 6-10 предложений. Первый абзац должен запускать главную драму, средние — показывать, как вокруг неё летит мусор и второстепенный шум, последний — давать короткий язвительный редакционный вывод. " +
	"Перед отправкой мысленно перечитай итог как редактор: проверь, что абзацы логично сцеплены, главная сцена не распалась на случайный набор эпизодов, имена не перепутаны, вывод следует из сообщений, а текст звучит живо и связно. Эту проверку не показывай. Никакого markdown, списков, заголовков, хештегов, служебных пометок, пояснений, дисклеймеров, рассуждений, анализа, черновиков и промежуточных шагов. Никогда не добавляй хештеги, даже если кажется, что они подходят к формату таблоида. Сразу выдавай только финальный текст summary. " +
	"Если сообщений слишком мало, они односложные, полностью мемные или контекста недостаточно, всё равно напиши краткое summary по тому, что есть, без выдумывания пропущенных связей."

type PromptBuilder struct {
	maxChars int
}

func NewPromptBuilder(maxChars int) PromptBuilder {
	return PromptBuilder{maxChars: maxChars}
}

func (b PromptBuilder) Build(windowStart, windowEnd time.Time, previousSummaries []string, prepared PreparedWindow, maxOutputTokens int) llm.GenerateSummaryInput {
	lines := make([]string, 0, len(prepared.Messages)+len(previousSummaries)+10)
	lines = append(lines,
		"Prompt sections:",
		"1. Previous published summaries are continuity context only. They may explain recurring characters, old jokes and ongoing storylines, but they are not evidence for new facts.",
		"2. Current messages are the only source of truth for new events, claims, quotes and conclusions.",
		"3. Write the final summary only from the current messages, using previous summaries only to avoid losing continuity.",
	)

	previousSummaries = normalizePreviousSummaries(previousSummaries)
	if len(previousSummaries) > 0 {
		lines = append(lines, "Previous published summaries for continuity, oldest to newest:")
		for i, previousSummary := range previousSummaries {
			lines = append(lines, fmt.Sprintf("Previous summary %d: %s", i+1, previousSummary))
		}
	} else {
		lines = append(lines, "Previous published summaries for continuity: none")
	}

	lines = append(lines,
		"Current message batch:",
		fmt.Sprintf("Message range time: %s - %s MSK", formatPromptDateTime(windowStart), formatPromptDateTime(windowEnd)),
		fmt.Sprintf("Meaningful messages: %d", prepared.MeaningfulCount),
		"Messages in chronological order:",
	)
	for _, message := range prepared.Messages {
		lines = append(lines, formatMessageLine(message))
	}

	return llm.GenerateSummaryInput{
		SystemPrompt:    systemPrompt,
		UserPrompt:      trimPrompt(strings.Join(lines, "\n"), b.maxChars),
		MaxOutputTokens: maxOutputTokens,
	}
}

func formatMessageLine(message PreparedMessage) string {
	sentAt := time.Unix(message.SentAt, 0).In(promptLocation).Format("15:04")
	line := fmt.Sprintf("[%s] %s (user_id %d)", sentAt, message.SenderName, message.SenderID)
	if message.ReplyToSenderName != "" || message.ReplyToText != "" {
		line += " in reply"
		if message.ReplyToSenderName != "" {
			line += " to " + message.ReplyToSenderName
		}
		if message.ReplyToText != "" {
			line += ": " + message.ReplyToText
		}
	}
	return line + " -> " + message.Text
}

func formatPromptDateTime(t time.Time) string {
	return t.In(promptLocation).Format("2006-01-02T15:04:05-07:00")
}

func normalizePreviousSummaries(summaries []string) []string {
	normalized := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		summary = normalizePreviousSummary(summary)
		if summary != "" {
			normalized = append(normalized, summary)
		}
	}
	return normalized
}

func normalizePreviousSummary(text string) string {
	text = stripSummaryHashtags(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func trimPrompt(prompt string, maxChars int) string {
	if len(prompt) <= maxChars {
		return prompt
	}

	lines := strings.Split(prompt, "\n")
	if len(lines) <= 3 {
		return prompt[:maxChars]
	}

	head := lines[:3]
	body := lines[3:]
	headText := strings.Join(head, "\n")
	if len(headText) >= maxChars {
		return headText[:maxChars]
	}

	remaining := maxChars - len(headText) - 1
	trimmedBody := trimBodyLines(body, remaining)
	if len(trimmedBody) == 0 {
		return headText
	}
	return headText + "\n" + strings.Join(trimmedBody, "\n")

}

func trimBodyLines(lines []string, maxChars int) []string {
	if len(lines) == 0 || maxChars <= 0 {
		return nil
	}

	for count := len(lines); count >= 1; count-- {
		selected := pickDistributedLines(lines, count)
		candidate := strings.Join(selected, "\n")
		if len(candidate) <= maxChars {
			return selected
		}
	}

	last := lines[len(lines)-1]
	if len(last) > maxChars {
		return []string{last[:maxChars]}
	}
	return []string{last}
}

func pickDistributedLines(lines []string, count int) []string {
	indexes := distributedIndexes(len(lines), count)
	selected := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		selected = append(selected, lines[idx])
	}
	return selected
}

func distributedIndexes(total, count int) []int {
	if total <= 0 || count <= 0 {
		return nil
	}
	if count >= total {
		indexes := make([]int, total)
		for i := range total {
			indexes[i] = i
		}
		return indexes
	}
	if count == 1 {
		return []int{total - 1}
	}

	indexes := make([]int, 0, count)
	last := -1
	for i := 0; i < count; i++ {
		idx := i * (total - 1) / (count - 1)
		if idx == last {
			continue
		}
		indexes = append(indexes, idx)
		last = idx
	}

	for len(indexes) < count {
		candidate := indexes[len(indexes)-1] + 1
		if candidate >= total {
			break
		}
		indexes = append(indexes, candidate)
	}

	return indexes
}
