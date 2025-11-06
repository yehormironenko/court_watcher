package parser

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"court-bot/types"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL    = "https://kluby.org"
	userAgent  = "Mozilla/5.0 (compatible; CourtsBot/1.0)"
	loginEmail = "wazap_by@mail.ru"
	loginPass  = "6282373"
)

var (
	lastRequest         time.Time
	authenticatedClient *http.Client
)

// AuthClient создает HTTP клиент с авторизацией
// initAuthClient создает HTTP клиент с cookies для авторизации
func initAuthClient() (*http.Client, error) {
	// Используем кешированный клиент если есть
	if authenticatedClient != nil {
		return authenticatedClient, nil
	}

	log.Println("🔐 Initializing authenticated client with cookies...")

	// Создаем клиент с cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
	}

	// Устанавливаем cookies для авторизации
	u, _ := url.Parse("https://kluby.org")
	cookies := []*http.Cookie{
		{
			Name:   "kluby_org",
			Value:  os.Getenv("KLUBY_ORG"),
			Domain: ".kluby.org",
			Path:   "/",
		},
		{
			Name:   "kluby_autolog",
			Value:  os.Getenv("KLUBY_AUTOLOG"),
			Domain: ".kluby.org",
			Path:   "/",
		},
		{
			Name:   "kluby_remember",
			Value:  "1",
			Domain: ".kluby.org",
			Path:   "/",
		},
	}
	jar.SetCookies(u, cookies)

	log.Println("✅ Using authenticated client with cookies")

	authenticatedClient = client
	return client, nil
}

// KeepCookiesAlive делает периодический пинг для поддержания активности куков
func KeepCookiesAlive() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		client, err := initAuthClient()
		if err != nil {
			log.Printf("⚠️ Cookie ping failed: error initializing client: %v", err)
			continue
		}

		// Делаем простой GET запрос на главную страницу
		resp, err := client.Get("https://kluby.org/")
		if err != nil {
			log.Printf("⚠️ Cookie ping failed: %v", err)
			continue
		}
		resp.Body.Close()

		log.Printf("✅ Cookie ping successful (status: %d)", resp.StatusCode)
	}
}

// cleanCourtName очищает название корта
// "Hala 1 Hala tenis" -> "Hala 1"
// "Kort 3 ziemny otwart Korty odkryte" -> "Kort 3"
func cleanCourtName(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return name
	}

	// Ищем паттерн "Hala N" или "Kort N"
	for i := 0; i < len(words)-1; i++ {
		if (words[i] == "Hala" || words[i] == "Kort") && len(words) > i+1 {
			return words[i] + " " + words[i+1]
		}
	}

	// Если не нашли, возвращаем первые 2 слова
	if len(words) >= 2 {
		return words[0] + " " + words[1]
	}

	return name
}

// normalizeTime преобразует время к формату "HH:MM" (добавляет ведущий 0 если нужно)
func normalizeTime(t string) string {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return t
	}

	hour := parts[0]
	minute := parts[1]

	// Добавляем ведущий 0 к часу если нужно
	if len(hour) == 1 {
		hour = "0" + hour
	}

	// Добавляем ведущий 0 к минуте если нужно
	if len(minute) == 1 {
		minute = "0" + minute
	}

	return hour + ":" + minute
}

// rateLimit добавляет задержку между запросами
func rateLimit() {
	delay := time.Duration(200+rand.Intn(301)) * time.Millisecond
	elapsed := time.Since(lastRequest)

	if elapsed < delay {
		time.Sleep(delay - elapsed)
	}
	lastRequest = time.Now()
}

// Storage interface для избежания циклической зависимости
type Storage interface {
	GetDistricts() ([]string, error)
	SaveDistricts(districts []string) error
	GetCourts(districts []string) ([]byte, error)
	SaveCourts(districts []string, courts interface{}) error
}

// FetchWarsawDistricts загружает список районов Варшавы из kluby.org
// Использует Redis кеш если доступен
func FetchWarsawDistricts(store Storage) ([]string, error) {
	// Проверяем кеш
	if store != nil {
		cached, err := store.GetDistricts()
		if err == nil && cached != nil {
			log.Printf("📍 Loaded %d districts from cache", len(cached))
			return cached, nil
		}
	}

	// Кеша нет, парсим сайт
	log.Println("🌐 Fetching districts from kluby.org...")
	rateLimit()

	// Инициализируем авторизованный клиент
	client, err := initAuthClient()
	if err != nil {
		return nil, err
	}

	districtsURL := baseURL + "/tenis/kluby/warszawa"
	req, err := http.NewRequest("GET", districtsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	districts := make([]string, 0)
	seen := make(map[string]bool)

	// Ищем ссылки на районы в "Lista dzielnic"
	doc.Find("a[href*='/tenis/kluby/warszawa/']").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		// Извлекаем название района из текста ссылки
		district := strings.TrimSpace(s.Text())

		// Проверяем что это действительно район (не пустое и имеет правильный формат URL)
		if district != "" && district != "Warszawa" && strings.Contains(href, "/tenis/kluby/warszawa/") && !seen[district] {
			districts = append(districts, district)
			seen[district] = true
		}
	})

	log.Printf("📍 Found %d districts in Warsaw", len(districts))

	// Сохраняем в кеш
	if store != nil {
		if err := store.SaveDistricts(districts); err != nil {
			log.Printf("⚠️ Failed to cache districts: %v", err)
		}
	}

	return districts, nil
}

// FetchCourts загружает список кортов из kluby.org для выбранных районов
// Использует Redis кеш если доступен
func FetchCourts(districts []string, store Storage) ([]types.Court, error) {
	// Проверяем кеш
	if store != nil {
		cached, err := store.GetCourts(districts)
		if err == nil && cached != nil {
			var courts []types.Court
			if json.Unmarshal(cached, &courts) == nil {
				log.Printf("🎾 Loaded %d courts from cache", len(courts))
				return courts, nil
			}
		}
	}

	// Кеша нет, парсим сайт
	log.Println("🌐 Fetching courts from kluby.org...")
	allCourts := make([]types.Court, 0)
	seen := make(map[string]bool) // дедупликация

	for _, district := range districts {
		log.Printf("🔍 Fetching courts for district: %s", district)

		courts, err := fetchCourtsForDistrict(district)
		if err != nil {
			log.Printf("⚠️ Error fetching courts for %s: %v", district, err)
			continue
		}

		for _, court := range courts {
			// Дедупликация по ID
			if !seen[court.ID] {
				allCourts = append(allCourts, court)
				seen[court.ID] = true
			}
		}
	}

	log.Printf("✅ Total courts found: %d", len(allCourts))

	// Сохраняем в кеш
	if store != nil {
		if err := store.SaveCourts(districts, allCourts); err != nil {
			log.Printf("⚠️ Failed to cache courts: %v", err)
		}
	}

	return allCourts, nil
}

// districtToSlug конвертирует название района в URL slug
func districtToSlug(district string) string {
	slug := strings.ToLower(district)

	// Польские символы → латиница
	replacements := map[string]string{
		"ą": "a", "ć": "c", "ę": "e", "ł": "l",
		"ń": "n", "ó": "o", "ś": "s", "ź": "z", "ż": "z",
		" ": "-", "–": "-", "—": "-",
	}

	for old, new := range replacements {
		slug = strings.ReplaceAll(slug, old, new)
	}

	return slug
}

// fetchCourtsForDistrict загружает корты для конкретного района
func fetchCourtsForDistrict(district string) ([]types.Court, error) {
	rateLimit()

	// Инициализируем авторизованный клиент
	client, err := initAuthClient()
	if err != nil {
		return nil, err
	}

	// URL страницы района: /tenis/kluby/warszawa/[slug]
	districtSlug := districtToSlug(district)
	districtURL := fmt.Sprintf("%s/tenis/kluby/warszawa/%s", baseURL, districtSlug)

	req, err := http.NewRequest("GET", districtURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	courts := make([]types.Court, 0)
	seen := make(map[string]bool)

	// Парсим карточки кортов
	// Структура: <a href="/court-name"><img/><h4>Name</h4><p>Address</p></a>
	// Категории спорта: <a href="/sport/..."><img/><h3>SPORT_NAME</h3></a>
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}

		// Пропускаем внешние ссылки и служебные страницы
		if strings.HasPrefix(href, "http") ||
			strings.HasPrefix(href, "//") ||
			strings.Contains(href, "regulamin") ||
			strings.Contains(href, "static") ||
			href == "/" || href == "" {
			return
		}

		// Пропускаем ссылки на спортивные категории (они имеют формат /sport/kluby/...)
		if strings.Contains(href, "/tenis/") ||
			strings.Contains(href, "/padel/") ||
			strings.Contains(href, "/squash/") ||
			strings.Contains(href, "/badminton/") ||
			strings.Contains(href, "/pickleball/") ||
			strings.Contains(href, "/golf/") ||
			strings.Contains(href, "/bilard/") {
			return
		}

		// Пропускаем навигационные ссылки
		if strings.Contains(href, "/kluby/") {
			return
		}

		// Пропускаем события, турниры и другие пути (они содержат "/" в href после первого символа)
		// Корты имеют простой формат: /court-slug (одно слово или слова через дефис)
		// События/турниры: /zapisy/123, /turnieje/456
		trimmedHref := strings.TrimPrefix(href, "/")
		if strings.Contains(trimmedHref, "/") {
			return
		}

		// Категории спорта используют h3, корты используют h4
		// Пропускаем все ссылки с h3 (это категории спорта)
		if s.Find("h3").Length() > 0 {
			return
		}

		// Проверяем что внутри есть h4 (название корта)
		heading := s.Find("h4").First()
		if heading.Length() == 0 {
			return
		}

		// Извлекаем название корта
		name := strings.TrimSpace(heading.Text())
		if name == "" || len(name) < 3 {
			return
		}

		// Пропускаем подозрительно короткие названия (обычно тестовые/неактивные корты)
		// Реальные корты имеют нормальные названия типа "Park Tennis Academy", "OSIR Bemowo"
		if len(name) <= 4 {
			// Исключения: известные короткие названия могут быть добавлены сюда если нужно
			return
		}

		// Пропускаем названия с датами в скобках - это события (например "(2026-03-01)")
		if strings.Contains(name, "(202") {
			return
		}

		// Проверяем возможно ли резервация
		reservation := s.Find("span").First()
		if reservation.Text() != "REZERWUJ" {
			return
		}

		// Извлекаем адрес (если есть)
		address := ""
		addressPara := s.Find("p").First()
		if addressPara.Length() > 0 {
			address = strings.TrimSpace(addressPara.Text())
		}

		// Фильтруем по расстоянию - если больше 50 км, вероятно ошибка
		if strings.Contains(address, " km)") {
			// Ищем число перед " km)"
			parts := strings.Split(address, " km)")
			if len(parts) > 0 {
				distPart := parts[0]
				lastSpace := strings.LastIndex(distPart, "(")
				if lastSpace != -1 && lastSpace < len(distPart)-1 {
					distStr := strings.TrimSpace(distPart[lastSpace+1:])
					distStr = strings.ReplaceAll(distStr, ",", ".")
					var dist float64
					if _, err := fmt.Sscanf(distStr, "%f", &dist); err == nil {
						if dist > 50.0 {
							return // Пропускаем корты дальше 50 км (например asd2 с 6129 км)
						}
					}
				}
			}
		}

		// Очищаем href от query параметров
		courtID := strings.Split(href, "?")[0]
		courtID = strings.TrimPrefix(courtID, "/")

		// Проверяем дубликаты
		if courtID == "" || seen[courtID] {
			return
		}

		seen[courtID] = true
		court := types.Court{
			ID:       courtID,
			Name:     name,
			District: district,
		}

		courts = append(courts, court)
	})

	log.Printf("  → Found %d courts in %s", len(courts), district)
	return courts, nil
}

// CheckCourtSchedule проверяет график конкретного корта на заданную дату
// courtID - ID корта (например "umacieja")
// date - дата в формате "2025-11-05"
// timeFrom, timeTo - диапазон времени (например "08:00", "22:00")
func CheckCourtSchedule(courtID, date, timeFrom, timeTo string) ([]types.Slot, error) {
	rateLimit()

	// Инициализируем авторизованный клиент
	client, err := initAuthClient()
	if err != nil {
		return nil, err
	}

	// Пробуем сначала страницу резервации (может не требовать логина)
	reserveURL := fmt.Sprintf("%s/%s/rezerwacje?data_grafiku=%s&dyscyplina=1", baseURL, courtID, date)
	log.Printf("  → Trying reservations page: %s", reserveURL)

	req, err := http.NewRequest("GET", reserveURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	// Теперь открываем страницу графика
	scheduleURL := fmt.Sprintf("%s/%s/grafik?data_grafiku=%s&dyscyplina=1&strona=0", baseURL, courtID, date)
	log.Printf("  → Fetching schedule page: %s", scheduleURL)

	req, err = http.NewRequest("GET", scheduleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Логируем статус и cookies для отладки
	log.Printf("  → Response status: %d", resp.StatusCode)
	if jar := client.Jar; jar != nil {
		cookies := jar.Cookies(req.URL)
		log.Printf("  → Using %d cookies", len(cookies))
	}

	// Читаем и парсим HTML
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}

	// Временная отладка для проблемных страниц
	tableCount := doc.Find("table").Length()
	rezerwujCount := doc.Find("a[href*='rezerwuj']").Length()

	// Проверяем, требуется ли авторизация для просмотра графика
	// Ищем конкретное сообщение, а не ссылку в меню
	bodyStr := string(bodyBytes)
	requiresLogin := strings.Contains(bodyStr, "Grafik widoczny po zalogowaniu") ||
		strings.Contains(bodyStr, "widoczny po zalogowaniu") ||
		strings.Contains(bodyStr, "Musisz się zalogować")

	if requiresLogin {
		log.Printf("  ⚠️ This court requires login - skipping (court: %s)", courtID)
		return []types.Slot{}, nil
	}

	// Если нет таблиц или ссылок, выводим часть HTML для отладки
	if tableCount == 0 || rezerwujCount == 0 {
		log.Printf("  ⚠️ Warning: Found %d tables, %d 'rezerwuj' links", tableCount, rezerwujCount)
		log.Printf("  → No available slots found for this court")
	}

	slots := make([]types.Slot, 0)
	seen := make(map[string]bool)

	// Парсим название клуба из заголовка страницы
	clubName := ""

	// Пробуем извлечь из title
	doc.Find("title").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Text())
		// Формат: "Nazwa Klubu - Rezerwacje ONLINE | Kluby.org"
		if strings.Contains(title, " - ") {
			parts := strings.Split(title, " - ")
			if len(parts) > 0 {
				clubName = strings.TrimSpace(parts[0])
			}
		}
	})

	// Если не нашли в title, ищем в заголовках
	if clubName == "" {
		doc.Find("h1, h2, h3").Each(func(i int, s *goquery.Selection) {
			if clubName == "" {
				text := strings.TrimSpace(s.Text())
				if text != "" &&
					!strings.Contains(strings.ToLower(text), "grafik") &&
					!strings.Contains(strings.ToLower(text), "kluby.org") &&
					len(text) > 3 {
					clubName = text
				}
			}
		})
	}

	if clubName == "" {
		clubName = courtID // fallback
	}

	log.Printf("  → Club name: %s", clubName)

	// Ищем таблицу с графиком (она имеет id="grafik")
	doc.Find("table#grafik").Each(func(i int, table *goquery.Selection) {
		// Получаем заголовки столбцов (названия кортов) только из thead
		courtTypes := make([]string, 0)
		table.Find("thead tr").First().Find("th").Each(func(j int, th *goquery.Selection) {
			// Берем только видимый текст, убираем все лишнее
			courtType := strings.TrimSpace(th.Text())
			// Убираем множественные пробелы и переносы строк
			courtType = strings.Join(strings.Fields(courtType), " ")
			courtTypes = append(courtTypes, courtType)
		})

		// Track rowspans: map[rowIndex][colIndex] = remainingRows
		// When a cell has rowspan, it occupies the next N-1 rows in that column
		rowspanTracker := make(map[int]map[int]int)

		// Парсим строки таблицы (временные слоты)
		table.Find("tbody tr, tr").Each(func(rowIndex int, tr *goquery.Selection) {
			// Первая ячейка - время
			timeCell := tr.Find("td").First()
			slotTime := strings.TrimSpace(timeCell.Text())

			// Проверяем формат времени (HH:MM)
			if !strings.Contains(slotTime, ":") {
				return
			}

			// Нормализуем время к формату HH:MM
			slotTime = normalizeTime(slotTime)

			// Initialize tracker for this row if needed
			if rowspanTracker[rowIndex] == nil {
				rowspanTracker[rowIndex] = make(map[int]int)
			}

			// Check if time is in range (but still process rowspans even if not)
			inTimeRange := slotTime >= timeFrom && slotTime <= timeTo

			// Get all physical cells in this row
			cells := tr.Find("td")

			// Track which logical column we're currently at
			logicalColIndex := 0

			// Iterate through physical cells
			cells.Each(func(physicalIndex int, td *goquery.Selection) {
				// Skip to next unoccupied logical column
				for logicalColIndex < len(courtTypes) {
					// Check if this logical column is occupied by a rowspan from a previous row
					isOccupied := false
					for prevRowIndex := 0; prevRowIndex < rowIndex; prevRowIndex++ {
						if remaining, exists := rowspanTracker[prevRowIndex][logicalColIndex]; exists && remaining > (rowIndex-prevRowIndex) {
							isOccupied = true
							break
						}
					}

					if !isOccupied {
						break // Found next unoccupied column
					}
					logicalColIndex++ // This column is occupied, try next one
				}

				if logicalColIndex >= len(courtTypes) {
					return // No more logical columns
				}

				// Now we have: physical cell 'td' maps to logical column 'logicalColIndex'

				// First cell is time column - skip it
				if logicalColIndex == 0 {
					logicalColIndex++
					return
				}

				// Check for rowspan attribute and track it
				rowspanStr, hasRowspan := td.Attr("rowspan")
				if hasRowspan {
					rowspan := 1
					fmt.Sscanf(rowspanStr, "%d", &rowspan)
					if rowspan > 1 {
						rowspanTracker[rowIndex][logicalColIndex] = rowspan
					}
				}

				// Сначала проверяем текст ячейки на "Zarezerwowane"
				cellText := strings.TrimSpace(td.Text())
				if strings.Contains(cellText, "Zarezerwowane") ||
					strings.Contains(cellText, "zarezerwowane") {
					logicalColIndex++ // Move to next logical column for next physical cell
					return            // Слот забронирован
				}

				// Ищем ссылку "Rezerwuj"
				link := td.Find("a[href*='rezerwuj']")
				if link.Length() == 0 {
					logicalColIndex++ // Move to next logical column
					return            // Слот занят или недоступен
				}

				linkText := strings.TrimSpace(link.Text())
				if !strings.Contains(strings.ToLower(linkText), "rezerwuj") {
					logicalColIndex++
					return
				}

				// Only create slots for rows in the time range
				if !inTimeRange {
					logicalColIndex++
					return
				}

				// Получаем href для бронирования
				href, exists := link.Attr("href")
				if !exists {
					logicalColIndex++
					return
				}

				// Определяем тип корта из заголовка столбца
				courtType := ""
				if logicalColIndex < len(courtTypes) {
					courtType = courtTypes[logicalColIndex]
				}

				// Пропускаем открытые корты (проверяем ДО очистки!)
				courtTypeLower := strings.ToLower(courtType)
				if strings.Contains(courtTypeLower, "otwarte") ||
					strings.Contains(courtTypeLower, "odkryte") ||
					strings.Contains(courtTypeLower, "odkryt") ||
					strings.Contains(courtTypeLower, "otwart") {
					logicalColIndex++
					return
				}

				// Очищаем название корта
				courtType = cleanCourtName(courtType)

				// Создаем слот
				slot := types.Slot{
					ClubID:    courtID,
					ClubName:  clubName,
					CourtType: courtType,
					TypeID:    courtID, // используем courtID как typeID
					Date:      date,
					Time:      slotTime,
					Duration:  2, // по умолчанию 2 часа
					Price:     "0,00",
					URL:       baseURL + href,
				}

				// Дедупликация
				uniqueID := slot.UniqueID()
				if !seen[uniqueID] {
					slots = append(slots, slot)
					seen[uniqueID] = true
				}

				// Move to next logical column for next physical cell
				logicalColIndex++
			})
		})
	})

	log.Printf("  → Found %d available slots for %s on %s (time range: %s-%s)", len(slots), courtID, date, timeFrom, timeTo)
	return slots, nil
}
