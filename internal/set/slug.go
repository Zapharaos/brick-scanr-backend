package set

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/text/unicode/norm"
)

// GetSlug returns the slug for the set
func (l *Locale) GetSlug() string {
	if l.Slug != "" {
		return l.Slug
	}
	return l.BricklinkSlug
}

// GenerateSlugDefault creates a URL-friendly slug for the set based on its name and number, with normalization and fallback logic
func (c *Core) GenerateSlugDefault() {
	var number string

	// Handle set number: prefer explicit Number, otherwise extract first numeric part from BricklinkNumber (format: "<numbers>-<numbers>")
	if c.Number != "" {
		number = c.Number
	} else {
		raw := strings.TrimSpace(c.BricklinkNumber)
		// try to extract the first sequence of digits
		reNum := regexp.MustCompile(`\d+`)
		if m := reNum.FindString(raw); m != "" {
			number = m
		} else {
			// fallback: take substring before '-' if present, otherwise use the whole trimmed string
			if idx := strings.Index(raw, "-"); idx != -1 {
				number = raw[:idx]
			} else {
				number = raw
			}
		}
	}

	// Handle name: normalize (remove diacritics), lower-case, replace non-alphanumeric chars with '-' and trim/collapse dashes
	name := c.BricklinkName
	// Normalize to NFD to separate diacritics, then drop them
	name = norm.NFD.String(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.Is(unicode.Mn, r) {
			// skip diacritic marks
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()
	name = strings.ToLower(name)

	// Replace any sequence of characters that are not a-z or 0-9 with a single '-'
	re := regexp.MustCompile(`[^a-z0-9]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	// Compose slug
	if number != "" && name != "" {
		c.BricklinkSlug = name + "-" + number
	} else if number != "" {
		c.BricklinkSlug = number
	} else {
		c.BricklinkSlug = name
	}
}

// RedisBuildKeySlug creates a Redis key for looking up Set by slug
func RedisBuildKeySlug(slug string) string {
	return fmt.Sprintf("set:slug:%s", slug)
}

// RedisSetSetIDForSlug stores the set uuid.UUID in Redis by its slug
// Uses distributed locking to ensure only one UUID is generated per slug across concurrent requests
func RedisSetSetIDForSlug(ctx context.Context, set Locale, updateTTL bool) error {
	slug := set.Slug
	if slug == "" {
		slug = set.BricklinkSlug
	}

	key := RedisBuildKeySlug(slug)

	// Marshal set to JSON
	setIdJSON, err := json.Marshal(set.ID)
	if err != nil {
		zap.L().Error("failed to marshal setID to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
		)
		return err
	}

	// Determine GetTTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Set
	} else {
		ttl = redis.KeepTTL
	}

	return redis.Set(ctx, key, setIdJSON, ttl, true)
}

// RedisGetSetIDBySlug retrieves the set uuid.UUID from Redis by its slug
// Uses distributed locking when key doesn't exist to ensure consistency during concurrent writes
func RedisGetSetIDBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	key := RedisBuildKeySlug(slug)
	data, err := redis.Get(ctx, key, true) // Use lock on cache miss
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("slug", slug),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	// Found cached data, unmarshal it
	var setID uuid.UUID
	if setID, err = uuid.Parse(data); err != nil {
		zap.L().Error(
			"failed to parse set ID",
			zap.String("slug", slug),
			zap.Error(err),
		)
		return uuid.Nil, err
	}

	return setID, nil
}
