package interviewer

import (
	"context"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// Модель получает служебные директивы («заведи стадию», «сделай ревью») в том же
// канале, что и реплики кандидата, и голос отвечает именно content'ом. Модель,
// которая вместо выполнения пересказывает инструкцию, озвучивает её кандидату —
// это слышно сразу и выглядит как сломанное интервью (проверено на живом стенде).
//
// Договор: такой ответ не озвучиваем. Один раз запрашиваем заново с поправкой;
// если модель повторила цитату — отдаём нейтральный вопрос и пишем ERROR.
const (
	// directiveStageEntry — директива «вход на стадию». Одна строка отсюда
	// уходит в buildMessages и оттуда же берётся признак цитаты.
	directiveStageEntry = "Кандидат перешёл на эту стадию. Заведи её: представь формат и задай первый вопрос (или, для Live-Code, объяви задачу)."

	// directiveFixMessage — правка, досылаемая перед повторным запросом.
	directiveFixMessage = "Ты дословно процитировал служебную инструкцию — её слышит кандидат. Так нельзя: не пересказывай её, а выполни. Задай кандидату вопрос по его последнему ответу."

	// guardHoldLimit — сколько символов набираем перед проверкой, если граница
	// фразы так и не пришла (защита от потока без знаков препинания).
	guardHoldLimit = 240
)

// leaksDirective — цитата ли это служебной директивы. Ищем два характерных
// куска, а не всю строку: опечатка в середине фразы цитату не отменяет, а
// «Кандидат перешёл»/«Заведи её» из речи интервьюера в принципе выпадают редко
// (ложное срабатывание стоит один лишний запрос к модели, не испорченное интервью).
func leaksDirective(text string) bool {
	return strings.Contains(text, "Кандидат перешёл на эту стадию") ||
		strings.Contains(text, "Заведи её")
}

// clauseReady — готова ли первая озвучиваемая фраза: есть граница предложения
// (TTS режет ответ по фразам и синтезирует по ним) или набран лимит.
func clauseReady(s string) bool {
	if len([]rune(s)) >= guardHoldLimit {
		return true
	}
	return strings.ContainsAny(s, ".!?\n")
}

// chatGuarded — i.llm.Chat + договор выше. Возвращает текст, который можно
// озвучивать и писать в историю.
func (i *Interviewer) chatGuarded(ctx context.Context, req llm.Request) (string, error) {
	resp, err := i.llm.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	if !leaksDirective(resp.Content) {
		return resp.Content, nil
	}
	i.log.Error("interviewer: модель процитировала служебную директиву — запрашиваю заново",
		"chars", len([]rune(resp.Content)))
	req.Messages = append(append([]llm.Message{}, req.Messages...),
		llm.Message{Role: llm.RoleUser, Content: directiveFixMessage})
	again, err := i.llm.Chat(ctx, req)
	if err == nil && !leaksDirective(again.Content) {
		return again.Content, nil
	}
	// second leak (or the retry failed): the directive must never reach TTS
	i.log.Error("interviewer: директива процитирована и после повтора — озвучиваю нейтральный вопрос",
		"err", err)
	return llm.MockReply, nil
}
