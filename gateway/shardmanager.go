package gateway

import (
	"github.com/go-redis/redis"
	"github.com/rxdn/gdl/cache"
)

type ShardManager interface {
	Connect() error
	IsWhitelabel() bool
	getRedis() *redis.Client
	getCache() *cache.PgCache
	onFatalError(token string, err error)
}
