package set

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/database"
	"github.com/Zapharaos/brick-scanr-backend/internal/redis"
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type FetchStatus int

const (
	FetchStatusPending FetchStatus = iota
	FetchStatusFetching
	FetchStatusCompleted
	FetchStatusFailed
)

type FetchErrorStep int

const (
	FetchErrorUnknown FetchErrorStep = iota + 1
	FetchErrorInitCache
	FetchErrorDetailsCache
	FetchErrorBatchCache
	FetchErrorFinalCache
	FetchErrorFetchInventory
	FetchErrorFetchPrices
)

type FetchError struct {
	Message string         `json:"message"`
	Step    FetchErrorStep `json:"step"`
}

// Locale represents the locale-specific information of a Lego set.
type Locale struct {
	Core

	// Fetching status and error
	FetchStatus FetchStatus `json:"fetch_status"`
	FetchError  *FetchError `json:"fetch_error,omitempty"`

	// Details from Lego
	Status          utils.Status `json:"status"`
	Name            string       `json:"name"`
	Slug            string       `json:"slug"`
	LegoURL         string       `json:"lego_url"`
	InstructionsURL string       `json:"instructions_url"`
	Price           utils.Price  `json:"price"`
}

// HasValidPrice checks if the set price is within the freshness window
func (l *Locale) HasValidPrice() bool {
	return l.Price.IsValid(database.DB().Redis().TTLS.SetPriceFreshness)
}

// BuildLegoURL constructs the LEGO product URL based on the set's slug and the provided locale
func (l *Locale) BuildLegoURL(tag language.Tag) {
	l.LegoURL = "https://www.lego.com/" + tag.String() + "/product/" + l.Slug
}

// BuildInstructionsURL constructs the LEGO building instructions URL based on the set's number and the provided locale
func (l *Locale) BuildInstructionsURL(tag language.Tag) {
	l.InstructionsURL = "https://www.lego.com/" + tag.String() + "/service/building-instructions/" + l.Number
}

// RedisBuildKeyLocale creates a Redis key for looking up Locale by UUID and tag
func RedisBuildKeyLocale(identifier uuid.UUID, tag language.Tag) string {
	return fmt.Sprintf("set:%s:%s", identifier, tag.String())
}

// RedisGetLocale retrieves a Locale from Redis by its UUID and tag
func RedisGetLocale(ctx context.Context, setID uuid.UUID, tag language.Tag, withCore bool) (Locale, bool, error) {

	if withCore {
		// Use MGET to fetch both Core and Locale in a single atomic operation
		// allowPartial=true allows us to get Core even if Locale doesn't exist yet
		coreKey := RedisBuildKeyCore(setID)
		localeKey := RedisBuildKeyLocale(setID, tag)

		results, err := redis.MGet(ctx, []string{coreKey, localeKey}, true)
		if err != nil {
			zap.L().Error(
				"failed to fetch core and locale from redis",
				zap.String("setID", setID.String()),
				zap.String("tag", tag.String()),
				zap.Error(err),
			)
			return Locale{}, false, err
		}

		// Unmarshal Core data
		var core Core
		coreFound := false
		if coreData, ok := results[coreKey]; ok {
			if err = json.Unmarshal([]byte(coreData), &core); err != nil {
				zap.L().Error(
					"failed to unmarshal core data",
					zap.String("setID", setID.String()),
					zap.Error(err),
				)
				return Locale{}, false, err
			}
			coreFound = true
		}

		// If Core is not found, return error
		if !coreFound {
			return Locale{}, false, redis.ErrKeyNotFound
		}

		// Unmarshal Locale data (optional - if not found, return empty locale with valid core)
		var locale Locale
		localeFound := false
		if localeData, ok := results[localeKey]; ok {
			if err = json.Unmarshal([]byte(localeData), &locale); err != nil {
				zap.L().Warn(
					"failed to unmarshal locale data, returning core with empty locale",
					zap.String("setID", setID.String()),
					zap.String("tag", tag.String()),
					zap.Error(err),
				)
				locale = Locale{}
			} else {
				localeFound = true
			}
		}

		// Merge Core data into Locale (even if locale was empty/missing)
		locale.Core = core
		return locale, localeFound, nil
	}

	key := RedisBuildKeyLocale(setID, tag)
	data, err := redis.Get(ctx, key)
	if err != nil {
		zap.L().Error(
			"failed to fetch data from redis",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Locale{}, false, err
	}

	// Found cached data, unmarshal it
	var cache Locale
	if err = json.Unmarshal([]byte(data), &cache); err != nil {
		zap.L().Error(
			"failed to unmarshal cached data",
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Locale{}, false, err
	}

	return cache, true, nil
}

// RedisSetLocale stores a Locale in Redis by its UUID and tag
func RedisSetLocale(ctx context.Context, set Locale, tag language.Tag, updateCore, updateTTL bool) error {
	localeKey := RedisBuildKeyLocale(set.ID, tag)

	if updateCore {
		// Use transaction to set Core + Locale atomically
		coreKey := RedisBuildKeyCore(set.ID)
		lockKey := redis.BuildLockKey(coreKey)

		// Determine TTL
		var ttl time.Duration
		if updateTTL {
			ttl = database.DB().Redis().TTLS.Set
		} else {
			ttl = redis.KeepTTL
		}

		// Prepare Core data
		coreJSON, err := json.Marshal(set.Core)
		if err != nil {
			zap.L().Error("failed to marshal core to JSON",
				zap.Error(err),
				zap.String("set_id", set.ID.String()),
			)
			return err
		}

		// Prepare Locale data (without Core)
		localeCopy := set
		localeCopy.Core = Core{}
		localeJSON, err := json.Marshal(localeCopy)
		if err != nil {
			zap.L().Error("failed to marshal locale to JSON",
				zap.Error(err),
				zap.String("set_id", set.ID.String()),
			)
			return err
		}

		// Execute transaction
		return redis.Transaction(ctx, lockKey, true, func(pipe goredis.Pipeliner) error {
			pipe.Set(ctx, coreKey, coreJSON, ttl)
			pipe.Set(ctx, localeKey, localeJSON, ttl)
			return nil
		})
	}

	// Clear the Core data to avoid storing it in the Locale cache
	set.Core = Core{}

	// Marshal set to JSON
	setJSON, err := json.Marshal(set)
	if err != nil {
		zap.L().Error("failed to marshal set to JSON",
			zap.Error(err),
			zap.String("set_id", set.ID.String()),
		)
		return err
	}

	// Determine TTL
	var ttl time.Duration
	if updateTTL {
		ttl = database.DB().Redis().TTLS.Set
	} else {
		ttl = redis.KeepTTL
	}

	return redis.Set(ctx, localeKey, setJSON, ttl, true)
}

// RedisGetLocaleByBricklinkID retrieves the Set from Redis by its Bricklink ID
// Returns the Set, its remaining GetTTL, or an error if not found
func RedisGetLocaleByBricklinkID(ctx context.Context, bricklinkID string, tag language.Tag) (Locale, time.Duration, error) {
	// Use MGET to fetch bricklinkID mapping, Core, and Locale in a single operation
	bricklinkKey := RedisBuildKeyBricklinkIDToSetID(bricklinkID)

	// First, get the setID from bricklinkID
	setIDStr, err := redis.Get(ctx, bricklinkKey)
	if err != nil {
		return Locale{}, redis.KeyNotFound, err
	}

	var setID uuid.UUID
	if setID, err = uuid.Parse(setIDStr); err != nil {
		zap.L().Error(
			"failed to parse set ID",
			zap.String("bricklinkID", bricklinkID),
			zap.Error(err),
		)
		return Locale{}, redis.KeyNotFound, err
	}

	if setID == uuid.Nil {
		return Locale{}, redis.KeyNotFound, redis.ErrKeyNotFound
	}

	// Now fetch Core and Locale using MGET with allowPartial=true
	// This allows us to get the Core even if the Locale doesn't exist yet
	coreKey := RedisBuildKeyCore(setID)
	localeKey := RedisBuildKeyLocale(setID, tag)

	results, err := redis.MGet(ctx, []string{coreKey, localeKey}, true)
	if err != nil {
		zap.L().Error(
			"failed to fetch core and locale from redis by bricklink ID",
			zap.String("bricklinkID", bricklinkID),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Locale{}, redis.KeyNotFound, err
	}

	// Unmarshal Core data
	var core Core
	coreFound := false
	if coreData, ok := results[coreKey]; ok {
		if err = json.Unmarshal([]byte(coreData), &core); err != nil {
			zap.L().Error(
				"failed to unmarshal core data",
				zap.String("bricklinkID", bricklinkID),
				zap.String("setID", setID.String()),
				zap.Error(err),
			)
			return Locale{}, redis.KeyNotFound, err
		}
		coreFound = true
	}

	// If Core is not found, we cannot proceed - this means the BrickLink ID mapping is stale
	if !coreFound {
		zap.L().Warn(
			"Core data not found for bricklink ID (stale mapping)",
			zap.String("bricklinkID", bricklinkID),
			zap.String("setID", setID.String()),
		)
		return Locale{}, redis.KeyNotFound, redis.ErrKeyNotFound
	}

	// Unmarshal Locale data (optional - if not found, we'll return empty locale with valid core)
	var locale Locale
	if localeData, ok := results[localeKey]; ok {
		if err = json.Unmarshal([]byte(localeData), &locale); err != nil {
			// If locale unmarshal fails, log but continue with empty locale
			zap.L().Warn(
				"failed to unmarshal locale data, returning core with empty locale",
				zap.String("bricklinkID", bricklinkID),
				zap.String("setID", setID.String()),
				zap.String("tag", tag.String()),
				zap.Error(err),
			)
			locale = Locale{}
		}
	}

	// Merge Core data into Locale (even if locale was empty/missing)
	locale.Core = core

	// Get remaining TTL for the core key
	ttl, err := redis.GetTTL(ctx, coreKey)
	if err != nil {
		zap.L().Error(
			"failed to get TTL for cached data",
			zap.String("bricklinkID", bricklinkID),
			zap.String("setID", setID.String()),
			zap.Error(err),
		)
		return Locale{}, redis.KeyNotFound, err
	}

	return locale, ttl, nil
}
