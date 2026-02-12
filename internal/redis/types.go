package redis

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

const KeepTTL = redis.KeepTTL

const KeyNotFound = -1

var (
	ErrKeyNotFound      = errors.New("key not found")
	ErrFailedToGetKey   = errors.New("failed to get key")
	ErrFailedToSetKey   = errors.New("failed to set key")
	ErrFailedToCheckKey = errors.New("failed to check key existence")
	ErrFailedToGetTTL   = errors.New("failed to get GetTTL")
)
