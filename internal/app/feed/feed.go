// Package feed хранит случайные сотни карточек отдельно от JWT blacklist.
package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const BatchSize = 100
const TTL = 24 * time.Hour

var ErrUnavailable = errors.New("хранилище ленты недоступно")

type Source interface {
	RandomPublished(context.Context, int) ([]ds.CloudTariff, error)
	PublishedCard(context.Context, uint, uint) (*ds.CloudTariff, error)
}
type Service struct {
	Client *redis.Client
	Source Source
}
type Item struct {
	Position int            `json:"position"`
	Tariff   ds.CloudTariff `json:"tariff"`
}
type Meta struct {
	SessionID  string `json:"session_id"`
	RedisKey   string `json:"redis_key"`
	BatchID    string `json:"batch_id"`
	BatchStart int    `json:"batch_start"`
	BatchSize  int    `json:"batch_size"`
	Position   int    `json:"position"`
}
type Query struct {
	Position int
	ID       uint
	Next     bool
	Init     bool
}

// Один Redis-lock на браузер/пользователя защищает генерацию от двойных запросов
// React StrictMode и параллельных вкладок. Lua удаляет только собственный lock.
func (s *Service) lock(ctx context.Context, key string) (func(), error) {
	token := uuid.NewString()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		ok, err := s.Client.SetNX(ctx, key, token, 15*time.Second).Result()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if ok {
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				s.Client.Eval(releaseCtx, `if redis.call('GET',KEYS[1]) == ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{key}, token)
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, ErrUnavailable
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (s *Service) Card(ctx context.Context, session string, viewer uint, q Query) (*ds.CloudTariff, Meta, error) {
	base := "feed:guest:" + session
	if viewer > 0 {
		base = fmt.Sprintf("feed:user:%d:%s", viewer, session)
	}
	key, metaKey := base+":cards", base+":meta"
	meta := Meta{SessionID: session, RedisKey: key}
	unlock, err := s.lock(ctx, base+":lock")
	if err != nil {
		return nil, meta, err
	}
	defer unlock()
	raw, err := s.Client.HGet(ctx, metaKey, "json").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, meta, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &meta); err != nil {
			return nil, meta, err
		}
	}
	makeBatch := func(start int) error {
		rows, e := s.Source.RandomPublished(ctx, BatchSize)
		if e != nil {
			return e
		}
		// Защита от некорректного источника: в Redis никогда нет повторных ID.
		seen := map[uint]bool{}
		values := make([]any, 0, len(rows))
		for _, row := range rows {
			if seen[row.TariffID] {
				continue
			}
			seen[row.TariffID] = true
			b, e := json.Marshal(Item{Position: start + len(values), Tariff: row})
			if e != nil {
				return e
			}
			values = append(values, string(b))
		}
		meta = Meta{SessionID: session, RedisKey: key, BatchID: uuid.NewString(), BatchStart: start, BatchSize: len(values)}
		b, e := json.Marshal(meta)
		if e != nil {
			return e
		}
		_, e = s.Client.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Del(ctx, key)
			if len(values) > 0 {
				p.RPush(ctx, key, values...)
				p.Expire(ctx, key, TTL)
			}
			p.HSet(ctx, metaKey, "json", string(b), "size", len(values), "start", start)
			p.Expire(ctx, metaKey, TTL)
			return nil
		})
		if e != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		return nil
	}
	if raw == "" || meta.BatchSize == 0 {
		if err = makeBatch(1); err != nil {
			return nil, meta, err
		}
	}
	if meta.BatchSize == 0 {
		if q.ID > 0 && !q.Next {
			row, e := s.Source.PublishedCard(ctx, q.ID, viewer)
			return row, meta, e
		}
		return nil, meta, nil
	}
	position := q.Position
	if position == 0 {
		position = meta.BatchStart
	}
	if q.ID > 0 && q.Next {
		// Старые ссылки ?id=...&next=true тоже идут по Redis-порядку.
		values, e := s.Client.LRange(ctx, key, 0, -1).Result()
		if e != nil {
			return nil, meta, fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		for _, v := range values {
			var item Item
			if e = json.Unmarshal([]byte(v), &item); e != nil {
				return nil, meta, e
			}
			if item.Tariff.TariffID == q.ID {
				position = item.Position + 1
				break
			}
		}
	}
	if q.ID > 0 && !q.Next {
		// Открытая из плитки карточка не меняет случайную сотню.
		row, e := s.Source.PublishedCard(ctx, q.ID, viewer)
		meta.Position = 0
		if e == nil {
			values, e2 := s.Client.LRange(ctx, key, 0, -1).Result()
			if e2 != nil {
				return nil, meta, fmt.Errorf("%w: %v", ErrUnavailable, e2)
			}
			for _, v := range values {
				var item Item
				if json.Unmarshal([]byte(v), &item) == nil && item.Tariff.TariffID == q.ID {
					meta.Position = item.Position
					break
				}
			}
		}
		return row, meta, e
	}
	// Не более двух сотен за запрос, даже если все карточки успели удалить.
	for attempts := 0; attempts <= 2*BatchSize; attempts++ {
		end := meta.BatchStart + meta.BatchSize
		if position < meta.BatchStart || position > end {
			return nil, meta, &repository.InputError{Message: "Номер вне текущей сотни; откройте ленту заново"}
		}
		if position == end {
			if err = makeBatch(end); err != nil {
				return nil, meta, err
			}
			if meta.BatchSize == 0 {
				return nil, meta, nil
			}
		}
		value, e := s.Client.LIndex(ctx, key, int64(position-meta.BatchStart)).Result()
		if e != nil {
			return nil, meta, fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		var item Item
		if e = json.Unmarshal([]byte(value), &item); e != nil {
			return nil, meta, e
		}
		// Порядок и снимки карточек находятся в Redis; одна проверка БД обновляет
		// лайки/владельца и исключает уже удалённые или снятые с публикации услуги.
		row, e := s.Source.PublishedCard(ctx, item.Tariff.TariffID, viewer)
		if errors.Is(e, repository.ErrTariffNotFound) {
			position++
			continue
		}
		if e != nil {
			return nil, meta, e
		}
		meta.Position = position
		_, e = s.Client.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Expire(ctx, key, TTL)
			p.Expire(ctx, metaKey, TTL)
			p.HSet(ctx, metaKey, "last_position", strconv.Itoa(position))
			return nil
		})
		if e != nil {
			return nil, meta, fmt.Errorf("%w: %v", ErrUnavailable, e)
		}
		return row, meta, nil
	}
	return nil, meta, nil
}
