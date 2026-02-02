package handlers

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var weekDays = []struct {
	Code string
	Name string
}{
	{"Mon", "Понедельник"},
	{"Tue", "Вторник"},
	{"Wed", "Среда"},
	{"Thu", "Четверг"},
	{"Fri", "Пятница"},
	{"Sat", "Суббота"},
	{"Sun", "Воскресенье"},
}

func (h *Handler) SendDaysSelection(chatID int64) {
	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		return
	}

	msg := tgbotapi.NewMessage(chatID, "📅 Шаг 3/4: Выбери дни недели\n\nВ какие дни искать свободные корты?")
	msg.ReplyMarkup = h.buildDaysKeyboard(sub.Days)
	h.Bot.Send(msg)
}

func (h *Handler) buildDaysKeyboard(selectedDays []string) tgbotapi.InlineKeyboardMarkup {
	selected := make(map[string]bool)
	for _, d := range selectedDays {
		selected[d] = true
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, day := range weekDays {
		label := day.Name
		if selected[day.Code] {
			label = "✅ " + day.Name
		}
		btn := tgbotapi.NewInlineKeyboardButtonData(label, "toggle_day:"+day.Code)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	// Кнопки быстрого выбора
	allWeekBtn := tgbotapi.NewInlineKeyboardButtonData("Вся неделя", "days_all")
	weekdaysBtn := tgbotapi.NewInlineKeyboardButtonData("Будни (Пн-Пт)", "days_weekdays")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(allWeekBtn, weekdaysBtn))

	done := tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "days_done")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(done))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) HandleDayToggle(cq *tgbotapi.CallbackQuery, day string) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	// Toggle выбранного дня
	found := false
	newDays := make([]string, 0, len(sub.Days))
	for _, d := range sub.Days {
		if d == day {
			found = true
		} else {
			newDays = append(newDays, d)
		}
	}
	if found {
		sub.Days = newDays
	} else {
		sub.Days = append(sub.Days, day)
	}
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить выбор."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildDaysKeyboard(sub.Days))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Обновлено"))
}

func (h *Handler) HandleDaysAll(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	sub.Days = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить выбор."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildDaysKeyboard(sub.Days))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Вся неделя выбрана"))
}

func (h *Handler) HandleDaysWeekdays(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	sub.Days = []string{"Mon", "Tue", "Wed", "Thu", "Fri"}
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить выбор."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildDaysKeyboard(sub.Days))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Будни выбраны"))
}

func (h *Handler) HandleDaysDone(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	if len(sub.Days) == 0 {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Выбери хотя бы один день"))
		return
	}

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Дни выбраны"))
	h.SendTimeSelection(chatID)
}

// Шаг 4: Выбор времени - начало
func (h *Handler) SendTimeSelection(chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "⏰ Шаг 4/4: Выбери время\n\nСначала выбери удобный вариант или настрой свое время:")
	msg.ReplyMarkup = h.buildTimePresetsKeyboard()
	h.Bot.Send(msg)
}

func (h *Handler) buildTimePresetsKeyboard() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	// Быстрые варианты
	morning := tgbotapi.NewInlineKeyboardButtonData("🌅 Утро (08:00-12:00)", "time_preset:08:00-12:00")
	afternoon := tgbotapi.NewInlineKeyboardButtonData("☀️ День (12:00-17:00)", "time_preset:12:00-17:00")
	evening := tgbotapi.NewInlineKeyboardButtonData("🌆 Вечер (17:00-22:00)", "time_preset:17:00-22:00")
	allDay := tgbotapi.NewInlineKeyboardButtonData("🌍 Весь день (08:00-22:00)", "time_preset:08:00-22:00")

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(morning))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(afternoon))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(evening))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(allDay))

	// Кнопка для детального выбора
	customBtn := tgbotapi.NewInlineKeyboardButtonData("⚙️ Настроить свое время", "time_custom")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(customBtn))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// Обработка быстрых пресетов
func (h *Handler) HandleTimePreset(cq *tgbotapi.CallbackQuery, timeRange string) {
	chatID := cq.Message.Chat.ID

	parts := strings.Split(timeRange, "-")
	if len(parts) != 2 {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Неверный формат времени"))
		return
	}

	timeFrom, timeTo := parts[0], parts[1]

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	sub.TimeFrom = timeFrom
	sub.TimeTo = timeTo
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить время."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Время выбрано"))
	h.SendSubscriptionSummary(chatID)
}

// Начало кастомного выбора времени
func (h *Handler) HandleTimeCustom(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, ""))
	h.SendTimeFromSelection(chatID, 0) // Начинаем с offset=0 (08:00)
}

// Выбор времени "от" с пагинацией
func (h *Handler) SendTimeFromSelection(chatID int64, offset int) {
	msg := tgbotapi.NewMessage(chatID, "⏰ Выбери время начала:")
	msg.ReplyMarkup = h.buildTimeSlotKeyboard(offset, "time_from")
	h.Bot.Send(msg)
}

// Генерация временных слотов (08:00 - 22:00 с шагом 30 минут)
var timeSlots = []string{
	"08:00", "08:30", "09:00", "09:30", "10:00", "10:30",
	"11:00", "11:30", "12:00", "12:30", "13:00", "13:30",
	"14:00", "14:30", "15:00", "15:30", "16:00", "16:30",
	"17:00", "17:30", "18:00", "18:30", "19:00", "19:30",
	"20:00", "20:30", "21:00", "21:30", "22:00",
}

func (h *Handler) buildTimeSlotKeyboard(offset int, prefix string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	// Показываем 6 слотов за раз (по 2 кнопки в ряд)
	slotsPerPage := 6
	start := offset
	end := offset + slotsPerPage
	if end > len(timeSlots) {
		end = len(timeSlots)
	}

	// Кнопки со временем (по 2 в ряд)
	for i := start; i < end; i += 2 {
		var row []tgbotapi.InlineKeyboardButton
		btn1 := tgbotapi.NewInlineKeyboardButtonData(timeSlots[i], prefix+":"+timeSlots[i])
		row = append(row, btn1)

		if i+1 < end {
			btn2 := tgbotapi.NewInlineKeyboardButtonData(timeSlots[i+1], prefix+":"+timeSlots[i+1])
			row = append(row, btn2)
		}
		rows = append(rows, row)
	}

	// Навигация
	var navRow []tgbotapi.InlineKeyboardButton
	if offset > 0 {
		prevBtn := tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", prefix+"_nav:"+fmt.Sprintf("%d", offset-slotsPerPage))
		navRow = append(navRow, prevBtn)
	}
	if end < len(timeSlots) {
		nextBtn := tgbotapi.NewInlineKeyboardButtonData("Вперед ▶️", prefix+"_nav:"+fmt.Sprintf("%d", offset+slotsPerPage))
		navRow = append(navRow, nextBtn)
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// Обработка навигации для "время от"
func (h *Handler) HandleTimeFromNav(cq *tgbotapi.CallbackQuery, offset string) {
	chatID := cq.Message.Chat.ID
	var off int
	fmt.Sscanf(offset, "%d", &off)

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildTimeSlotKeyboard(off, "time_from"))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, ""))
}

// Обработка выбора "время от"
func (h *Handler) HandleTimeFrom(cq *tgbotapi.CallbackQuery, timeFrom string) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	sub.TimeFrom = timeFrom
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить время."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, fmt.Sprintf("✅ Время начала: %s", timeFrom)))
	h.SendTimeToSelection(chatID, 0, timeFrom)
}

// Выбор времени "до" с пагинацией
func (h *Handler) SendTimeToSelection(chatID int64, offset int, timeFrom string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("⏰ Выбери время окончания:\n\nВремя начала: *%s*", timeFrom))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = h.buildTimeSlotKeyboard(offset, "time_to")
	h.Bot.Send(msg)
}

// Обработка навигации для "время до"
func (h *Handler) HandleTimeToNav(cq *tgbotapi.CallbackQuery, offset string) {
	chatID := cq.Message.Chat.ID
	var off int
	fmt.Sscanf(offset, "%d", &off)

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildTimeSlotKeyboard(off, "time_to"))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, ""))
}

// Обработка выбора "время до"
func (h *Handler) HandleTimeTo(cq *tgbotapi.CallbackQuery, timeTo string) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	// Проверка, что время окончания больше времени начала
	if timeTo <= sub.TimeFrom {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Время окончания должно быть больше времени начала"))
		return
	}

	sub.TimeTo = timeTo
	if h.checkMode[chatID] {
		err = h.Store.SaveCheck(sub)
	} else {
		err = h.Store.Save(sub)
	}
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить время."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Время выбрано"))
	h.SendSubscriptionSummary(chatID)
}

func (h *Handler) SendSubscriptionSummary(chatID int64) {
	// Определяем режим и загружаем подписку
	isCheckMode := h.checkMode[chatID]
	sub, err := h.Store.GetCurrent(chatID) // ← GetCurrent вместо if/else

	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		return
	}

	var text string
	if isCheckMode {
		// Режим check - одноразовая проверка
		text = fmt.Sprintf(
			"🔍 Выполняю разовую проверку!\n\n"+
				"🏙 Районы: %s\n"+
				"🎾 Корты: %d выбрано\n"+
				"📅 Дни: %s\n"+
				"⏰ Время: %s - %s\n\n"+
				"Ищу доступные слоты...",
			strings.Join(sub.Districts, ", "),
			len(sub.Courts),
			formatDays(sub.Days),
			sub.TimeFrom,
			sub.TimeTo,
		)
	} else {
		// Режим subscribe - постоянная подписка
		text = fmt.Sprintf(
			"✅ Подписка настроена!\n\n"+
				"🏙 Районы: %s\n"+
				"🎾 Корты: %d выбрано\n"+
				"📅 Дни: %s\n"+
				"⏰ Время: %s - %s\n\n"+
				"Проверяю доступные слоты...",
			strings.Join(sub.Districts, ", "),
			len(sub.Courts),
			formatDays(sub.Days),
			sub.TimeFrom,
			sub.TimeTo,
		)
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, text))

	// Запускаем проверку для обоих режимов
	if h.Checker != nil {
		h.Checker.CheckSubscriptionNow(chatID)
	}

	// Если режим check, удаляем временную подписку после проверки
	if isCheckMode {
		go func() {
			// CheckSubscriptionNow запускается в goroutine и читает из Redis (checker.go:156-164)
			// Ждем пока checker прочитает подписку
			time.Sleep(2 * time.Second)

			if err := h.Store.DeleteCheck(chatID); err != nil {
				log.Printf("⚠️ Ошибка при удалении временной подписки: %v", err)
			} else {
				log.Printf("🗑️ Временная подписка удалена для chatID: %d", chatID)
			}

			// Очищаем флаг режима
			delete(h.checkMode, chatID)
		}()
	} else {
		// Очищаем флаг режима для subscribe тоже
		delete(h.checkMode, chatID)
	}
}
