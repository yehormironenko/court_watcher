package checker

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"court-bot/parser"
	"court-bot/storage"
	"court-bot/types"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Checker struct {
	Bot   *tgbotapi.BotAPI
	Store *storage.Storage
}

func New(bot *tgbotapi.BotAPI, store *storage.Storage) *Checker {
	return &Checker{
		Bot:   bot,
		Store: store,
	}
}

// Start запускает горутину для периодической проверки с адаптивным интервалом
func (c *Checker) Start() {
	log.Println("🔍 Checker service started")

	// Инициализируем кеш для существующих подписок без отправки уведомлений
	c.initializeExistingSubscriptions()

	// Адаптивный таймер: 20 минут днем, 3 часа ночью
	go c.adaptiveCheckLoop()
}

// initializeExistingSubscriptions инициализирует кеш для существующих подписок без отправки уведомлений
func (c *Checker) initializeExistingSubscriptions() {
	log.Println("🔄 Initializing cache for existing subscriptions...")

	// Получаем все активные подписки
	subscriptions, err := c.Store.List()
	if err != nil {
		log.Printf("⚠️ Error fetching subscriptions: %v", err)
		return
	}

	log.Printf("📋 Found %d existing subscriptions to initialize", len(subscriptions))

	for _, sub := range subscriptions {
		// Пропускаем неполные подписки
		if len(sub.Districts) == 0 || len(sub.Courts) == 0 || len(sub.Days) == 0 {
			continue
		}

		log.Printf("🔄 Initializing cache for chatID: %d", sub.ChatID)

		// Собираем все доступные слоты
		allSlots := c.findAvailableSlots(sub)

		// Фильтруем по выбранным кортам
		filteredSlots := c.filterBySelectedCourts(allSlots, sub.Courts)

		// Сохраняем в кеш БЕЗ отправки уведомлений
		c.Store.SaveLastSlots(sub.ChatID, filteredSlots)

		log.Printf("  ✅ Cached %d slots for chatID: %d", len(filteredSlots), sub.ChatID)
	}

	log.Println("✅ Cache initialization completed")
}

// adaptiveCheckLoop запускает проверки с адаптивным интервалом
func (c *Checker) adaptiveCheckLoop() {
	for {
		now := time.Now()
		hour := now.Hour()

		// С 1:00 до 8:00 - проверяем раз в 3 часа
		// С 8:00 до 1:00 - проверяем каждые 20 минут
		var sleepDuration time.Duration
		if hour >= 1 && hour < 8 {
			sleepDuration = 4 * time.Hour
			log.Println("😴 Night mode: next check in 3 hours")
		} else {
			sleepDuration = 20 * time.Minute
			log.Println("🔍 Day mode: next check in 20 minutes")
		}

		time.Sleep(sleepDuration)
		c.checkAll(false) // Периодическая проверка - только новые слоты
	}
}

// checkAll проверяет все подписки
// isInitial - true при первом запуске (отправляем все слоты), false при периодических проверках (только новые)
func (c *Checker) checkAll(isInitial bool) {
	log.Println("🔍 Running availability check...")

	// Получаем все активные подписки
	subscriptions, err := c.Store.List()
	if err != nil {
		log.Printf("⚠️ Error fetching subscriptions: %v", err)
		return
	}

	log.Printf("📋 Found %d active subscriptions", len(subscriptions))

	for _, sub := range subscriptions {
		c.checkSubscription(sub, isInitial)
	}
}

// checkSubscription проверяет одну подписку
func (c *Checker) checkSubscription(sub *storage.Subscription, isInitial bool) {
	// Пропускаем неполные подписки
	if len(sub.Districts) == 0 || len(sub.Courts) == 0 || len(sub.Days) == 0 {
		return
	}

	log.Printf("🔍 Checking subscription for chatID: %d", sub.ChatID)

	// Собираем все доступные слоты
	allSlots := c.findAvailableSlots(sub)

	// Фильтруем по выбранным кортам
	filteredSlots := c.filterBySelectedCourts(allSlots, sub.Courts)

	// Фильтруем слоты, которые уже прошли
	filteredSlots = c.filterPastSlots(filteredSlots)

	log.Printf("  → Found %d slots (after filtering by selected courts and removing past slots)", len(filteredSlots))

	if isInitial {
		// Первая проверка - отправляем все доступные слоты
		if len(filteredSlots) > 0 {
			c.sendNotification(sub.ChatID, filteredSlots, "🎾 Текущие доступные слоты:")
		}
		// Сохраняем состояние
		c.Store.SaveLastSlots(sub.ChatID, filteredSlots)
	} else {
		// Периодическая проверка - только новые слоты
		newSlots := c.findNewSlots(sub.ChatID, filteredSlots)
		if len(newSlots) > 0 {
			c.sendNotification(sub.ChatID, newSlots, "🆕 Появились новые слоты!")
			// Обновляем состояние
			c.Store.SaveLastSlots(sub.ChatID, filteredSlots)
		}
	}
}

// CheckSubscriptionNow проверяет конкретную подписку сразу (для использования после создания подписки)
func (c *Checker) CheckSubscriptionNow(chatID int64) {
	sub, err := c.Store.GetCurrent(chatID)
	if err != nil || sub == nil {
		log.Printf("⚠️ Error fetching subscription for chatID %d: %v", chatID, err)
		return
	}

	// Запускаем проверку в отдельной горутине, чтобы не блокировать ответ пользователю
	go c.checkSubscription(sub, true)
}

// findAvailableSlots ищет все доступные слоты для подписки
func (c *Checker) findAvailableSlots(sub *storage.Subscription) []types.Slot {
	allSlots := make([]types.Slot, 0)

	// Генерируем даты на 14 дней вперед для выбранных дней недели
	dates := c.generateDates(sub.Days, 14)

	// Для каждого корта
	for _, courtID := range sub.Courts {
		// Для каждой даты
		for _, date := range dates {
			// Один запрос на корт на день - получаем весь график
			slots, err := parser.CheckCourtSchedule(courtID, date, sub.TimeFrom, sub.TimeTo)
			if err != nil {
				log.Printf("⚠️ Error checking schedule for %s on %s: %v", courtID, date, err)
				continue
			}
			allSlots = append(allSlots, slots...)
		}
	}

	// Дедупликация по UniqueID
	seen := make(map[string]bool)
	uniqueSlots := make([]types.Slot, 0)
	for _, slot := range allSlots {
		id := slot.UniqueID()
		if !seen[id] {
			uniqueSlots = append(uniqueSlots, slot)
			seen[id] = true
		}
	}

	return uniqueSlots
}

// generateDates генерирует даты на следующие N дней для выбранных дней недели
func (c *Checker) generateDates(selectedDays []string, daysAhead int) []string {
	dates := make([]string, 0)
	now := time.Now()

	// Проверяем каждый день в периоде
	for i := 0; i < daysAhead; i++ {
		date := now.AddDate(0, 0, i)
		dayShort := date.Weekday().String()[:3] // "Mon", "Tue", etc.

		// Проверяем, входит ли этот день в выбранные
		for _, selectedDay := range selectedDays {
			if dayShort == selectedDay {
				dates = append(dates, date.Format("2006-01-02"))
				break
			}
		}
	}

	return dates
}

// generateTimeSlots генерирует временные слоты каждые 30 минут между from и to
func (c *Checker) generateTimeSlots(from, to string) []string {
	slots := make([]string, 0)

	// Парсим from и to
	fromTime, err := time.Parse("15:04", from)
	if err != nil {
		log.Printf("⚠️ Error parsing TimeFrom: %v", err)
		return slots
	}
	toTime, err := time.Parse("15:04", to)
	if err != nil {
		log.Printf("⚠️ Error parsing TimeTo: %v", err)
		return slots
	}

	// Генерируем слоты каждые 30 минут
	current := fromTime
	for current.Before(toTime) {
		slots = append(slots, current.Format("15:04"))
		current = current.Add(30 * time.Minute)
	}

	return slots
}

// filterBySelectedCourts фильтрует слоты по выбранным кортам
func (c *Checker) filterBySelectedCourts(slots []types.Slot, selectedCourts []string) []types.Slot {
	filtered := make([]types.Slot, 0)

	// Создаем мапу выбранных кортов для быстрого поиска
	courtsMap := make(map[string]bool)
	for _, courtID := range selectedCourts {
		courtsMap[courtID] = true
	}

	for _, slot := range slots {
		if courtsMap[slot.ClubID] {
			filtered = append(filtered, slot)
		}
	}

	return filtered
}

// filterPastSlots фильтрует слоты, которые уже прошли
func (c *Checker) filterPastSlots(slots []types.Slot) []types.Slot {
	filtered := make([]types.Slot, 0)
	now := time.Now()

	for _, slot := range slots {
		// Парсим дату и время слота
		slotDateTime, err := time.Parse("2006-01-02 15:04", slot.Date+" "+slot.Time)
		if err != nil {
			log.Printf("⚠️ Error parsing slot date/time: %v (date=%s, time=%s)", err, slot.Date, slot.Time)
			continue
		}

		// Проверяем, что слот в будущем (с запасом в 5 минут)
		if slotDateTime.After(now.Add(-5 * time.Minute)) {
			filtered = append(filtered, slot)
		}
	}

	return filtered
}

// findNewSlots находит новые слоты (которых не было в предыдущей проверке)
func (c *Checker) findNewSlots(chatID int64, currentSlots []types.Slot) []types.Slot {
	// Загружаем предыдущие слоты
	lastSlotsData, err := c.Store.GetLastSlots(chatID)
	if err != nil || lastSlotsData == nil {
		// Если нет предыдущих данных, считаем все слоты новыми
		return currentSlots
	}

	var lastSlots []types.Slot
	if err := json.Unmarshal(lastSlotsData, &lastSlots); err != nil {
		log.Printf("⚠️ Error unmarshaling last slots: %v", err)
		return currentSlots
	}

	// Создаем мапу предыдущих слотов
	lastSlotsMap := make(map[string]bool)
	for _, slot := range lastSlots {
		lastSlotsMap[slot.UniqueID()] = true
	}

	// Находим новые слоты
	newSlots := make([]types.Slot, 0)
	for _, slot := range currentSlots {
		if !lastSlotsMap[slot.UniqueID()] {
			newSlots = append(newSlots, slot)
		}
	}

	return newSlots
}

// sendNotification отправляет уведомление о доступных слотах
func (c *Checker) sendNotification(chatID int64, slots []types.Slot, header string) {
	if len(slots) == 0 {
		return
	}

	// Группируем слоты по клубам для более читабельного вывода
	clubSlots := make(map[string][]types.Slot)
	for _, slot := range slots {
		clubSlots[slot.ClubName] = append(clubSlots[slot.ClubName], slot)
	}

	// Формируем сообщение
	// Отправляем отдельное сообщение для каждого клуба
	for clubName, clubSlotsList := range clubSlots {
		var message strings.Builder
		message.WriteString(fmt.Sprintf("🎾 **%s**\n\n", clubName))

		for _, slot := range clubSlotsList {
			// Название корта уже очищено в парсере (cleanCourtName)
			courtName := strings.TrimSpace(slot.CourtType)

			message.WriteString(fmt.Sprintf(
				"%s %s - %s\n",
				slot.Date, slot.Time, courtName,
			))
		}

		msg := tgbotapi.NewMessage(chatID, message.String())
		msg.ParseMode = "Markdown"
		c.Bot.Send(msg)
	}

	log.Printf("✅ Notification sent to chatID: %d (%d slots)", chatID, len(slots))
}
