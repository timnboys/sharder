package gateway

import (
	"context"
	"github.com/go-redis/redis"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rxdn/gdl/cache"
	"github.com/rxdn/gdl/rest/ratelimit"
	"os"
	"strconv"
)

// Shard manager for public bot
type PublicShardManager struct {
	total, id   int
	token       string
	limiter     *ratelimit.Ratelimiter
	options     ShardOptions
	shards      map[int]*Shard
	cache       cache.PgCache
	redisClient *redis.Client
}

func NewPublicShardManager(options ShardOptions) (manager *PublicShardManager, err error) {
	manager = &PublicShardManager{
		shards:  make(map[int]*Shard),
		options: options,
	}

	manager.total, err = strconv.Atoi(os.Getenv("SHARDER_TOTAL"))
	if err != nil {
		return
	}

	manager.id, err = strconv.Atoi(os.Getenv("SHARDER_ID"))
	if err != nil {
		return
	}

	// cache
	{
		db, err := pgxpool.Connect(context.Background(), os.Getenv("SHARDER_CACHE_URI"))
		if err != nil {
			return manager, err
		}

		manager.cache = cache.NewPgCache(db, cache.CacheOptions{
			Guilds:   true,
			Users:    true,
			Members:  true,
			Channels: true,
			Roles:    true,
		})
	}

	// getRedis
	manager.redisClient, err = manager.buildRedisClient()
	if err != nil {
		return
	}

	manager.limiter = ratelimit.NewRateLimiter(ratelimit.NewRedisStore(manager.redisClient, "ratelimiter:public"), 1)

	// create shards
	for i := options.ShardCount.Lowest; i < options.ShardCount.Highest; i++ {
		shard := NewShard(manager, os.Getenv("SHARDER_TOKEN"), i, manager.limiter, manager.options)
		manager.shards[i] = &shard
	}

	return
}

func (sm *PublicShardManager) Connect() error {
	for _, shard := range sm.shards {
		go shard.EnsureConnect()
	}

	return nil
}

func (sm *PublicShardManager) IsWhitelabel() bool {
	return false
}

func (sm *PublicShardManager) getRedis() *redis.Client {
	return sm.redisClient
}

func (sm *PublicShardManager) getCache() *cache.PgCache {
	return &sm.cache
}

func (sm *PublicShardManager) onFatalError(token string, err error) {
	// TODO: Implement something?
}

func (sm *PublicShardManager) buildRedisClient() (client *redis.Client, err error) {
	threads, err := strconv.Atoi(os.Getenv("SHARDER_REDIS_THREADS"))
	if err != nil {
		return
	}

	options := &redis.Options{
		Network:      "tcp",
		Addr:         os.Getenv("SHARDER_REDIS_ADDR"),
		Password:     os.Getenv("SHARDER_REDIS_PASSWD"),
		PoolSize:     threads,
		MinIdleConns: threads,
	}

	client = redis.NewClient(options)

	// test conn
	return client, client.Ping().Err()
}
