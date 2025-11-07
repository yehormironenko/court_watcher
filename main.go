package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"court-bot/checker"
	"court-bot/handlers"
	"court-bot/parser"
	"court-bot/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var store *storage.Storage

func initStorage() {
	addr := os.Getenv("REDIS_ADDR")
	pass := os.Getenv("REDIS_PASSWORD")
	db := 0 // court-watcher
	store = storage.New(addr, pass, db)

	// тестируем подключение
	if err := store.Ping(); err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}
}

func main() {
	// Set timezone to Europe/Warsaw (CET/CEST)
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		log.Printf("⚠️ Failed to load Warsaw timezone: %v (using UTC)", err)
	} else {
		time.Local = loc
		log.Printf("🌍 Timezone set to Europe/Warsaw (current time: %s)", time.Now().Format("2006-01-02 15:04:05 MST"))
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("❌ TELEGRAM_BOT_TOKEN not set")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal(err)
	}

	bot.Debug = true
	log.Printf("🤖 Authorized on account %s", bot.Self.UserName)

	initStorage()

	// Загружаем список районов из kluby.org (с кешированием в Redis)
	log.Println("📍 Loading Warsaw districts...")
	if err := handlers.InitDistricts(store); err != nil {
		log.Printf("⚠️ Failed to load districts: %v (using fallback)", err)
	}

	// Запускаем периодический пинг куков (каждые 10 минут)
	log.Println("🍪 Starting cookie keepalive service...")
	go func() {
		// Делаем первый пинг сразу для проверки
		resp, _ := http.Get("https://kluby.org/")
		if resp != nil {
			resp.Body.Close()
		}
		// Запускаем периодический пинг
		parser.KeepCookiesAlive()
	}()

	// Запускаем сервис проверки доступности в отдельной горутине
	checkerService := checker.New(bot, store)
	go checkerService.Start()

	// Инициализируем обработчики (передаем checker для немедленной проверки после подписки)
	handler := handlers.New(bot, store, checkerService)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	log.Println("✅ Bot is running...")

	for update := range updates {
		if update.Message != nil {
			handleMessage(bot, handler, update.Message)
		} else if update.CallbackQuery != nil {
			handleCallback(handler, update.CallbackQuery)
		}
	}
}

func handleMessage(bot *tgbotapi.BotAPI, h *handlers.Handler, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		h.HandleStart(msg)

	case "subscribe":
		h.HandleSubscribe(msg)

	case "my_subs":
		h.HandleMySubscriptions(msg)

	case "cancel":
		h.HandleCancel(msg)

	default:
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Неизвестная команда. Попробуй /start"))
	}
}

func handleCallback(h *handlers.Handler, cq *tgbotapi.CallbackQuery) {
	if cq == nil || cq.Message == nil {
		return
	}

	data := cq.Data

	// Роутинг callback'ов
	switch {
	// Выбор районов
	case strings.HasPrefix(data, "toggle_district:"):
		district := strings.TrimPrefix(data, "toggle_district:")
		h.HandleDistrictToggle(cq, district)

	case data == "districts_done":
		h.HandleDistrictsDone(cq)

	// Выбор кортов (используем короткий префикс для обхода лимита callback_data)
	case strings.HasPrefix(data, "court:"):
		courtIndex := strings.TrimPrefix(data, "court:")
		h.HandleCourtToggle(cq, courtIndex)

	case data == "courts_done":
		h.HandleCourtsDone(cq)

	// Выбор дней
	case strings.HasPrefix(data, "toggle_day:"):
		day := strings.TrimPrefix(data, "toggle_day:")
		h.HandleDayToggle(cq, day)

	case data == "days_all":
		h.HandleDaysAll(cq)

	case data == "days_weekdays":
		h.HandleDaysWeekdays(cq)

	case data == "days_done":
		h.HandleDaysDone(cq)

	// Выбор времени - быстрые пресеты
	case strings.HasPrefix(data, "time_preset:"):
		timeRange := strings.TrimPrefix(data, "time_preset:")
		h.HandleTimePreset(cq, timeRange)

	// Выбор времени - кастомный выбор
	case data == "time_custom":
		h.HandleTimeCustom(cq)

	// Выбор "время от"
	case strings.HasPrefix(data, "time_from:"):
		timeFrom := strings.TrimPrefix(data, "time_from:")
		h.HandleTimeFrom(cq, timeFrom)

	// Навигация "время от"
	case strings.HasPrefix(data, "time_from_nav:"):
		offset := strings.TrimPrefix(data, "time_from_nav:")
		h.HandleTimeFromNav(cq, offset)

	// Выбор "время до"
	case strings.HasPrefix(data, "time_to:"):
		timeTo := strings.TrimPrefix(data, "time_to:")
		h.HandleTimeTo(cq, timeTo)

	// Навигация "время до"
	case strings.HasPrefix(data, "time_to_nav:"):
		offset := strings.TrimPrefix(data, "time_to_nav:")
		h.HandleTimeToNav(cq, offset)

	default:
		h.Bot.Request(tgbotapi.NewCallback(cq.ID, "Неизвестная команда"))
	}
}
