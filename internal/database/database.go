package database

// Databases holds all the database connections
type Databases struct {
	redis RedisDB
}

// Redis returns the redis database connection
func (db *Databases) Redis() RedisDB {
	return db.redis
}

// SetRedis sets the redis database connection while preserving other connections
func (db *Databases) SetRedis(redis RedisDB) {
	db.redis.Close()
	db.redis = redis
}

// CloseAll closes all database connections
func (db *Databases) CloseAll() {
	db.redis.Close()
}

var _databases Databases

// DB returns the global Databases struct
func DB() *Databases {
	return &_databases
}

// ReplaceGlobals replaces the global Databases struct with the provided one
func ReplaceGlobals(databases Databases) func() {
	prev := _databases
	_databases = databases
	return func() { _databases = prev }
}
