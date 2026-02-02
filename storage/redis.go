package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type Storage struct {
	client *redis.Client
}

func New(addr, password string, db int) *Storage {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,     // например: "localhost:6379" или "redis-xxxxx.upstash.io:6379"
		Password: password, // можно пустым
		DB:       db,
	})
	return &Storage{client: rdb}
}

type Subscription struct {
	ChatID    int64
	Districts []string
	Courts    []string // Court IDs from kluby.org
	Days      []string // ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
	TimeFrom  string   // "18:00"
	TimeTo    string   // "21:00"
}

// Save подписку в Redis
func (s *Storage) Save(sub *Subscription) error {
	key := fmt.Sprintf("sub:%d", sub.ChatID)
	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

// Get подписку по chat_id
func (s *Storage) Get(chatID int64) (*Subscription, error) {
	key := fmt.Sprintf("sub:%d", chatID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sub Subscription
	if err := json.Unmarshal([]byte(val), &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// List все подписки
func (s *Storage) List() ([]*Subscription, error) {
	keys, err := s.client.Keys(ctx, "sub:*").Result()
	if err != nil {
		return nil, err
	}

	var subs []*Subscription
	for _, key := range keys {
		val, err := s.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var sub Subscription
		if json.Unmarshal([]byte(val), &sub) == nil {
			subs = append(subs, &sub)
		}
	}
	return subs, nil
}

// Delete удаляет подписку
func (s *Storage) Delete(chatID int64) error {
	key := fmt.Sprintf("sub:%d", chatID)
	return s.client.Del(ctx, key).Err()
}

func (s *Storage) GetCurrent(chatID int64) (*Subscription, error) {
	// Сначала проверяем check-режим
	sub, err := s.GetCheck(chatID)
	if err != nil {
		return nil, err
	}
	if sub != nil {
		return sub, nil
	}

	// Если нет check-подписки, проверяем обычную
	return s.Get(chatID)
}

func (s *Storage) GetCheck(chatID int64) (*Subscription, error) {
	key := fmt.Sprintf("check:%d", chatID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sub Subscription
	if err := json.Unmarshal([]byte(val), &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// SaveCheck сохраняет временную проверку с TTL (5 минут как safety net)
func (s *Storage) SaveCheck(sub *Subscription) error {
	key := fmt.Sprintf("check:%d", sub.ChatID)
	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	// TTL = 5 минут для автоматической очистки незавершенных проверок
	return s.client.Set(ctx, key, data, 5*time.Minute).Err()
}

// DeleteCheck удаляет временную проверку
func (s *Storage) DeleteCheck(chatID int64) error {
	key := fmt.Sprintf("check:%d", chatID)
	return s.client.Del(ctx, key).Err()
}

func (s *Storage) Ping() error {
	return s.client.Ping(ctx).Err()
}

// ===== Кеширование районов =====

// SaveDistricts сохраняет список районов Варшавы в кеш (TTL: 72 часа)
func (s *Storage) SaveDistricts(districts []string) error {
	key := "cache:districts:warsaw"
	data, err := json.Marshal(districts)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 72*time.Hour).Err()
}

// GetDistricts получает список районов из кеша
func (s *Storage) GetDistricts() ([]string, error) {
	key := "cache:districts:warsaw"
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // кеш пуст
	}
	if err != nil {
		return nil, err
	}
	var districts []string
	if err := json.Unmarshal([]byte(val), &districts); err != nil {
		return nil, err
	}
	return districts, nil
}

// ===== Кеширование кортов =====

// SaveCourts сохраняет список кортов для районов в кеш (TTL: 1 час)
func (s *Storage) SaveCourts(districts []string, courts interface{}) error {
	// Создаем ключ из списка районов (отсортированный для консистентности)
	sortedDistricts := make([]string, len(districts))
	copy(sortedDistricts, districts)
	sort.Strings(sortedDistricts)

	key := fmt.Sprintf("cache:courts:%s", strings.Join(sortedDistricts, ","))
	data, err := json.Marshal(courts)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 24*time.Hour).Err() // 1 час
}

// GetCourts получает список кортов для районов из кеша
func (s *Storage) GetCourts(districts []string) ([]byte, error) {
	sortedDistricts := make([]string, len(districts))
	copy(sortedDistricts, districts)
	sort.Strings(sortedDistricts)

	key := fmt.Sprintf("cache:courts:%s", strings.Join(sortedDistricts, ","))
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // кеш пуст
	}
	if err != nil {
		return nil, err
	}
	return []byte(val), nil
}

// ===== Хранение состояния слотов для нотификаций =====

// SaveLastSlots сохраняет последние найденные слоты для подписки (TTL: 24 часа)
func (s *Storage) SaveLastSlots(chatID int64, slots interface{}) error {
	key := fmt.Sprintf("slots:%d", chatID)
	data, err := json.Marshal(slots)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// GetLastSlots получает последние слоты для подписки
func (s *Storage) GetLastSlots(chatID int64) ([]byte, error) {
	key := fmt.Sprintf("slots:%d", chatID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // нет сохраненных слотов
	}
	if err != nil {
		return nil, err
	}
	return []byte(val), nil
}
