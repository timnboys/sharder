package gateway

import (
	"context"
	"fmt"
	"github.com/TicketsBot/common/tokenchange"
	"github.com/TicketsBot/database"
	"github.com/go-redis/redis"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rxdn/gdl/cache"
	"github.com/rxdn/gdl/gateway/intents"
	"github.com/rxdn/gdl/objects/user"
	"github.com/rxdn/gdl/rest/ratelimit"
	"os"
	"strconv"
	"sync"
)

type WhitelabelShardManager struct {
	total, id int

	db    *database.Database
	cache cache.PgCache
	redis *redis.Client

	bots     map[uint64]Shard
	botsLock sync.RWMutex

	tokens     map[uint64]string // bot ID -> token
	tokensLock sync.RWMutex
}

func NewWhitelabelShardManager() (manager *WhitelabelShardManager, err error) {
	manager = &WhitelabelShardManager{
		bots:   make(map[uint64]Shard),
		tokens: make(map[uint64]string),
	}

	manager.total, err = strconv.Atoi(os.Getenv("SHARDER_TOTAL"))
	if err != nil {
		return
	}

	manager.id, err = strconv.Atoi(os.Getenv("SHARDER_ID"))
	if err != nil {
		return
	}

	// database
	{
		db, err := pgxpool.Connect(context.Background(), os.Getenv("SHARDER_PG_URI"))
		if err != nil {
			return manager, err
		}

		manager.db = database.NewDatabase(db)
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

	// redis
	if err = manager.connectRedis(); err != nil {
		return
	}

	return
}

func (sm *WhitelabelShardManager) connectRedis() (err error) {
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

	sm.redis = redis.NewClient(options)

	// test conn
	return sm.redis.Ping().Err()
}

func (sm *WhitelabelShardManager) Connect() error {
	bots, err := sm.db.Whitelabel.GetBotsBySharder(sm.total, sm.id)
	if err != nil {
		return err
	}

	for _, bot := range bots {
		sm.tokensLock.Lock()
		sm.tokens[bot.BotId] = bot.Token
		sm.tokensLock.Unlock()

		// create ratelimiter
		store := ratelimit.NewRedisStore(sm.redis, fmt.Sprintf("ratelimiter:%d", bot.BotId))
		rateLimiter := ratelimit.NewRateLimiter(store, 1)

		shard := NewShard(bot.Token, 0, rateLimiter, &sm.cache, sm.redis, ShardOptions{
			ShardCount: ShardCount{
				Total:   1,
				Lowest:  0,
				Highest: 1,
			},
			GuildSubscriptions: false,
			Presence:           user.BuildStatus(user.ActivityTypePlaying, "DM for help | t!help"),
			Intents: []intents.Intent{
				intents.Guilds,
				intents.GuildMembers,
				intents.GuildMessages,
				intents.GuildMessageReactions,
				intents.GuildWebhooks,
				intents.DirectMessages,
				intents.DirectMessageReactions,
			},
			LargeShardingBuckets: 1,
		})

		sm.botsLock.Lock()
		sm.bots[bot.BotId] = shard
		sm.botsLock.Unlock()

		go shard.EnsureConnect()
	}

	return nil
}

// ListenNewTokens before connect
func (sm *WhitelabelShardManager) ListenNewTokens() {
	ch := make(chan tokenchange.TokenChangeData)
	go tokenchange.ListenTokenChange(sm.redis, ch)

	for payload := range ch {
		if payload.OldId%uint64(sm.total) != uint64(sm.id) {
			continue
		}

		sm.botsLock.Lock()

		for id, shard := range sm.bots {
			if id == payload.OldId {
				shard.Kill()
				break
			}
		}

		sm.botsLock.Unlock()

		// TODO: Connect on new ID
	}
}
