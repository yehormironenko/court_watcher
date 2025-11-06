package handlers

import (
	"court-bot/parser"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// CourtInfo - минимальная информация о корте для кеша
type CourtInfo struct {
	ID   string
	Name string
}

// Кеш маппинга индексов кортов для обхода лимита Telegram callback_data (64 байта)
// chatID -> []CourtInfo (только ID и название)
var courtsIndexCache = make(map[int64][]CourtInfo)

func (h *Handler) SendCourtsSelection(chatID int64) {
	sub, err := h.Store.Get(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		return
	}

	districtsText := strings.Join(sub.Districts, ", ")

	// Показываем индикатор загрузки
	loadingMsg := tgbotapi.NewMessage(chatID, "🔄 Загружаю список кортов...")
	sentMsg, _ := h.Bot.Send(loadingMsg)

	// Получаем корты из kluby.org (с кешированием в Redis)
	courts, err := parser.FetchCourts(sub.Districts, h.Store)
	if err != nil {
		log.Printf("⚠️ Error fetching courts: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке кортов. Попробуй позже."))
		return
	}

	if len(courts) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не найдено кортов в выбранных районах."))
		return
	}

	// Удаляем сообщение о загрузке
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, sentMsg.MessageID)
	h.Bot.Send(deleteMsg)

	// Конвертируем в упрощенную структуру и сохраняем в кеш
	courtInfos := make([]CourtInfo, len(courts))
	for i, court := range courts {
		courtInfos[i] = CourtInfo{
			ID:   court.ID,
			Name: court.Name,
		}
	}
	courtsIndexCache[chatID] = courtInfos

	// Отправляем меню выбора кортов
	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("🎾 Шаг 2/4: Выбери корты\n\nРайоны: *%s*\nНайдено кортов: *%d*\n\nОтметь нужные корты:",
			districtsText, len(courtInfos)))
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = h.buildCourtsKeyboard(chatID, sub.Courts, courtInfos)
	h.Bot.Send(msg)
}

func (h *Handler) buildCourtsKeyboard(chatID int64, selectedCourts []string, availableCourts []CourtInfo) tgbotapi.InlineKeyboardMarkup {
	selected := make(map[string]bool)
	for _, c := range selectedCourts {
		selected[c] = true
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for idx, court := range availableCourts {
		// Ограничиваем длину названия для красоты
		name := court.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}

		label := name
		if selected[court.ID] {
			label = "✅ " + label
		}
		// Используем индекс вместо court.ID для обхода лимита 64 байта
		btn := tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("court:%d", idx))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	done := tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "courts_done")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(done))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) HandleCourtToggle(cq *tgbotapi.CallbackQuery, courtIndexStr string) {
	chatID := cq.Message.Chat.ID

	// Получаем индекс корта
	courtIndex, err := strconv.Atoi(courtIndexStr)
	if err != nil {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Неверный индекс"))
		return
	}

	// Получаем список кортов из кеша
	courtInfos, ok := courtsIndexCache[chatID]
	if !ok || courtIndex < 0 || courtIndex >= len(courtInfos) {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Корт не найден"))
		return
	}

	courtInfo := courtInfos[courtIndex]

	sub, err := h.Store.Get(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	// Toggle выбранного корта
	found := false
	newCourts := make([]string, 0, len(sub.Courts))
	for _, c := range sub.Courts {
		if c == courtInfo.ID {
			found = true
		} else {
			newCourts = append(newCourts, c)
		}
	}
	if found {
		sub.Courts = newCourts
	} else {
		sub.Courts = append(sub.Courts, courtInfo.ID)
	}

	if err := h.Store.Save(sub); err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Не удалось сохранить выбор корта."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	// Обновляем клавиатуру
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildCourtsKeyboard(chatID, sub.Courts, courtInfos))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Обновлено"))
}

func (h *Handler) HandleCourtsDone(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID

	sub, err := h.Store.Get(chatID)
	if err != nil || sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}

	if len(sub.Courts) == 0 {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Выбери хотя бы один корт"))
		return
	}

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Корты выбраны"))
	h.SendDaysSelection(chatID)
}
