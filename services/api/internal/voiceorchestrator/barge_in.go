package voiceorchestrator

// BargeInAction — результат barge-in-решения.
type BargeInAction int

const (
	// BargeInNone — TTS не активна (или речи нет): barge-in не применяется.
	BargeInNone BargeInAction = iota
	// BargeInIgnore — речь короче порога (BargeInMinSpeechMS): TTS НЕ прерываем.
	BargeInIgnore
	// BargeInTrigger — речь достаточной длительности: прерываем TTS.
	BargeInTrigger
)

// BargeInDecision — решение о barge-in (ЧИСТАЯ функция входов, без side-effects,
// без метрик): speechMS — общая длительность речи кандидата (мс), preRollMS —
// pre-roll (часть реплики ДО текущего окна, мс). Эффективная речь
// ms = speechMS - min(preRollMS, speechMS).
//
//   - TTS не активна / речи нет → BargeInNone;
//   - ms < BargeInMinSpeechMS → BargeInIgnore (прерывать не за что);
//   - иначе → BargeInTrigger.
//
// Метрики и фактическое StopTTS делает вызывающий (httpapi) по возвращённому
// action — здесь только решение.
func BargeInDecision(ttsActive bool, speechMS, preRollMS int) (action BargeInAction, ms int) {
	if !ttsActive || speechMS <= 0 {
		return BargeInNone, 0
	}
	if preRollMS > speechMS {
		preRollMS = speechMS
	}
	ms = speechMS - preRollMS
	if ms < BargeInMinSpeechMS {
		return BargeInIgnore, ms
	}
	return BargeInTrigger, ms
}
