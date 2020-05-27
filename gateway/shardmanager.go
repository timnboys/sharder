package gateway

import (
	"github.com/go-redis/redis"
)

type ShardManager interface {
	Connect() error
	IsWhitelabel() bool
	redis() *redis.Client
	onFatalError(token string, err error)
}
