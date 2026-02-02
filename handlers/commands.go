package handlers

import (
	"fmt"
	"strings"

	"court-bot/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// CheckerInterface определяет методы для работы с checker
type CheckerInterface interface {
	CheckSubscriptionNow(chatID int64)
}

type Handler struct {
	Bot       *tgbotapi.BotAPI
	Store     *storage.Storage
	Checker   CheckerInterface
	checkMode map[int64]bool
}

func New(bot *tgbotapi.BotAPI, store *storage.Storage, checker CheckerInterface) *Handler {
	return &Handler{
		Bot:       bot,
		Store:     store,
		Checker:   checker,
		checkMode: make(map[int64]bool),
	}
}

func (h *Handler) HandleStart(msg *tgbotapi.Message) {
	text := "👋 Привет! Я помогу тебе отслеживать свободные теннисные корты в Варшаве.\n\n" +
		"Доступные команды:\n" +
		"/subscribe — настроить подписку на уведомления\n" +
		"/my_subs — показать мои подписки\n" +
		"/get_current — проверить прямо сейчас (по подписке)\n" +
		"/cancel — отменить текущую подписку\n" +
		"/check — проверить доступные корты в определенное время"
	h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, text))
}

func (h *Handler) HandleSubscribe(msg *tgbotapi.Message) {
	h.checkMode[msg.Chat.ID] = false
	h.sendDistrictSelection(msg.Chat.ID)
}

func (h *Handler) HandleCheckCourts(msg *tgbotapi.Message) {
	h.checkMode[msg.Chat.ID] = true
	h.sendDistrictSelection(msg.Chat.ID)
}

func (h *Handler) HandleMySubscriptions(msg *tgbotapi.Message) {
	sub, err := h.Store.Get(msg.Chat.ID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ошибка при загрузке подписок."))
		return
	}

	if sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "У тебя пока нет активных подписок.\n\nИспользуй /subscribe чтобы создать подписку."))
		return
	}

	text := fmt.Sprintf("📬 Твоя подписка:\n\n"+
		"🏙 Районы: %s\n"+
		"🎾 Корты: %d выбрано\n"+
		"📅 Дни: %s\n"+
		"⏰ Время: %s - %s",
		strings.Join(sub.Districts, ", "),
		len(sub.Courts),
		formatDays(sub.Days),
		sub.TimeFrom,
		sub.TimeTo)

	h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, text))
}

func (h *Handler) HandleCancel(msg *tgbotapi.Message) {
	// Проверяем, есть ли подписка
	sub, err := h.Store.Get(msg.Chat.ID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ошибка при проверке подписки."))
		return
	}

	if sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "У тебя нет активной подписки.\n\nИспользуй /subscribe чтобы создать новую подписку."))
		return
	}

	// Удаляем подписку
	err = h.Store.Delete(msg.Chat.ID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Ошибка при удалении подписки."))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Подписка успешно отменена.\n\nТы больше не будешь получать уведомления о доступных кортах.\n\nЧтобы создать новую подписку, используй /subscribe"))
}

func formatDays(days []string) string {
	if len(days) == 0 {
		return "не выбраны"
	}
	if len(days) == 7 {
		return "все дни"
	}
	dayNames := map[string]string{
		"Mon": "Пн", "Tue": "Вт", "Wed": "Ср", "Thu": "Чт",
		"Fri": "Пт", "Sat": "Сб", "Sun": "Вс",
	}
	result := make([]string, 0, len(days))
	for _, d := range days {
		if name, ok := dayNames[d]; ok {
			result = append(result, name)
		}
	}
	return strings.Join(result, ", ")
}

func (h *Handler) HandleGetCurrent(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	// Проверяем наличие подписки
	sub, err := h.Store.Get(chatID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Ошибка при загрузке подписки."))
		return
	}

	if sub == nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "У тебя нет активной подписки.\n\nИспользуй /subscribe чтобы создать подписку или /check для разовой проверки."))
		return
	}

	// Проверяем что подписка полная (все параметры заданы)
	if len(sub.Districts) == 0 || len(sub.Courts) == 0 || len(sub.Days) == 0 || sub.TimeFrom == "" || sub.TimeTo == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Твоя подписка неполная.\n\nИспользуй /subscribe чтобы завершить настройку."))
		return
	}

	// Отправляем сообщение о начале проверки
	text := fmt.Sprintf("🔍 Проверяю доступность кортов по твоей подписке...\n\n"+
		"🏙 Районы: %s\n"+
		"🎾 Корты: %d выбрано\n"+
		"📅 Дни: %s\n"+
		"⏰ Время: %s - %s",
		strings.Join(sub.Districts, ", "),
		len(sub.Courts),
		formatDays(sub.Days),
		sub.TimeFrom,
		sub.TimeTo)

	h.Bot.Send(tgbotapi.NewMessage(chatID, text))

	// Запускаем проверку
	if h.Checker != nil {
		h.Checker.CheckSubscriptionNow(chatID)
	} else {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Сервис проверки временно недоступен."))
	}
}
