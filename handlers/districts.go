package handlers

import (
	"log"

	"court-bot/parser"
	"court-bot/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// districts будет загружен из kluby.org при инициализации
var districts []string

// InitDistricts загружает список районов Варшавы из kluby.org (с кешированием в Redis)
func InitDistricts(store *storage.Storage) error {
	var err error
	districts, err = parser.FetchWarsawDistricts(store)
	if err != nil {
		log.Printf("⚠️ Failed to fetch districts from kluby.org: %v", err)
		// Fallback на жестко закодированный список
		districts = []string{
			"Mokotów", "Wola", "Ursynów", "Śródmieście", "Ochota",
			"Żoliborz", "Praga Południe", "Praga Północ", "Bielany",
		}
		log.Printf("Using fallback district list (%d districts)", len(districts))
	}
	return err
}

// userSelections хранит временные выборы пользователей (district checkboxes)
var userSelections = make(map[int64]map[string]bool)

func (h *Handler) sendDistrictSelection(chatID int64) {
	if _, ok := userSelections[chatID]; !ok {
		userSelections[chatID] = make(map[string]bool)
	}

	msg := tgbotapi.NewMessage(chatID, "🏙 Шаг 1/4: Выбери районы Варшавы\n\nНажимай на районы, чтобы отметить нужные:")
	msg.ReplyMarkup = h.buildDistrictsKeyboard(chatID)
	h.Bot.Send(msg)
}

func (h *Handler) buildDistrictsKeyboard(chatID int64) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, d := range districts {
		selected := userSelections[chatID][d]
		label := d
		if selected {
			label = "✅ " + d
		}
		btn := tgbotapi.NewInlineKeyboardButtonData(label, "toggle_district:"+d)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	doneBtn := tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "districts_done")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(doneBtn))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *Handler) HandleDistrictToggle(cq *tgbotapi.CallbackQuery, district string) {
	chatID := cq.Message.Chat.ID

	if userSelections[chatID] == nil {
		userSelections[chatID] = make(map[string]bool)
	}
	userSelections[chatID][district] = !userSelections[chatID][district]

	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, cq.Message.MessageID, h.buildDistrictsKeyboard(chatID))
	h.Bot.Send(edit)
	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Обновлено"))
}

func (h *Handler) HandleDistrictsDone(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID

	selectedDistricts := make([]string, 0)
	for district, selected := range userSelections[chatID] {
		if selected {
			selectedDistricts = append(selectedDistricts, district)
		}
	}

	if len(selectedDistricts) == 0 {
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "⚠️ Выбери хотя бы один район"))
		return
	}

	sub, err := h.Store.GetCurrent(chatID)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при чтении подписки."))
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}
	if sub == nil {
		sub = &storage.Subscription{ChatID: chatID}
	}

	sub.Districts = selectedDistricts
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

	h.Bot.Request(tgbotapi.NewCallback(cq.ID, "✅ Районы выбраны"))
	h.SendCourtsSelection(chatID)
}
