package gateway

import (
	"context"
	"fmt"
	"github.com/TicketsBot/common/statusupdates"
	"github.com/TicketsBot/common/tokenchange"
	"github.com/TicketsBot/common/whitelabeldelete"
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

	db          *database.Database
	cache       cache.PgCache
	redisClient *redis.Client

	bots     map[uint64]*Shard
	botsLock sync.RWMutex

	tokens     map[uint64]string // bot ID -> token
	tokensLock sync.RWMutex
}

func NewWhitelabelShardManager() (manager *WhitelabelShardManager, err error) {
	manager = &WhitelabelShardManager{
		bots:   make(map[uint64]*Shard),
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

	fmt.Println("Connecting to DB...")

	// database
	{
		threads, err := strconv.Atoi(os.Getenv("DATABASE_THREADS"))
		if err != nil {
			panic(err)
		}

		connString := fmt.Sprintf(
			"postgres://%s:%s@%s/%s?pool_max_conns=%d",
			os.Getenv("DATABASE_USER"),
			os.Getenv("DATABASE_PASSWORD"),
			os.Getenv("DATABASE_HOST"),
			os.Getenv("DATABASE_NAME"),
			threads,
		)

		db, err := pgxpool.Connect(context.Background(), connString)
		if err != nil {
			return manager, err
		}

		manager.db = database.NewDatabase(db)
	}

	fmt.Println("Connected to DB, connecting to cache...")

	// cache
	{
		threads, err := strconv.Atoi(os.Getenv("CACHE_THREADS"))
		if err != nil {
			panic(err)
		}

		connString := fmt.Sprintf(
			"postgres://%s:%s@%s/%s?pool_max_conns=%d",
			os.Getenv("CACHE_USER"),
			os.Getenv("CACHE_PASSWORD"),
			os.Getenv("CACHE_HOST"),
			os.Getenv("CACHE_NAME"),
			threads,
		)

		db, err := pgxpool.Connect(context.Background(), connString)
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

	fmt.Println("Connected to cache, connecting to Redis...")

	// getRedis
	if err = manager.connectRedis(); err != nil {
		return
	}

	fmt.Println("Connected to Redis.")

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

	sm.redisClient = redis.NewClient(options)

	// test conn
	return sm.redisClient.Ping().Err()
}

func (sm *WhitelabelShardManager) Connect() error {
	bots, err := sm.db.Whitelabel.GetBotsBySharder(sm.total, sm.id)
	if err != nil {
		return err
	}

	for _, bot := range bots {
		sm.connectBot(bot)
	}

	return nil
}

func (sm *WhitelabelShardManager) IsWhitelabel() bool {
	return true
}

func (sm *WhitelabelShardManager) getRedis() *redis.Client {
	return sm.redisClient
}

func (sm *WhitelabelShardManager) getCache() *cache.PgCache {
	return &sm.cache
}

func (sm *WhitelabelShardManager) onFatalError(token string, err error) {
	// find bot id
	var id uint64
	sm.tokensLock.RLock()
	for botId, botToken := range sm.tokens {
		if botToken == token {
			id = botId
			break
		}
	}
	sm.tokensLock.RUnlock()

	// get user ID
	if bot, e := sm.db.Whitelabel.GetByBotId(id); e == nil { // TODO: Sentry
		sm.db.WhitelabelErrors.Append(bot.UserId, err.Error())
	}

	sm.db.Whitelabel.DeleteByToken(token)
}

// ListenNewTokens before connect
func (sm *WhitelabelShardManager) ListenNewTokens() {
	ch := make(chan tokenchange.TokenChangeData)
	go tokenchange.ListenTokenChange(sm.redisClient, ch)

	for payload := range ch {
		// check whether this shard has the old bot
		if payload.OldId%uint64(sm.total) == uint64(sm.id) {
			sm.botsLock.Lock()

			for id, shard := range sm.bots {
				if id == payload.OldId {
					shard.Kill()
					break
				}
			}

			sm.botsLock.Unlock()
		}

		// check whether this shard manager should run the new bot
		if payload.NewId%uint64(sm.total) == uint64(sm.id) {
			// connect new bot in a goroutine as to not block
			go sm.connectBot(database.WhitelabelBot{
				UserId: 0, // UserId is not used by connectBot
				BotId:  payload.NewId,
				Token:  payload.Token,
			})
		}
	}
}

func (sm *WhitelabelShardManager) connectBot(bot database.WhitelabelBot) {
	sm.tokensLock.Lock()
	sm.tokens[bot.BotId] = bot.Token
	sm.tokensLock.Unlock()

	// create ratelimiter
	store := ratelimit.NewRedisStore(sm.redisClient, fmt.Sprintf("ratelimiter:%d", bot.BotId))
	rateLimiter := ratelimit.NewRateLimiter(store, 1)

	shard := NewShard(sm, bot.Token, 0, rateLimiter, ShardOptions{
		ShardCount: ShardCount{
			Total:   1,
			Lowest:  0,
			Highest: 1,
		},
		GuildSubscriptions: false,
		Presence:           user.BuildStatus(user.ActivityTypePlaying, sm.getStatus(bot.BotId)),
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
	sm.bots[bot.BotId] = &shard
	sm.botsLock.Unlock()

	go shard.EnsureConnect()
}

func (sm *WhitelabelShardManager) getStatus(botId uint64) string {
	customStatus, err := sm.db.WhitelabelStatuses.Get(botId)
	if err != nil || customStatus == "" {
		return "DM for help | t!help"
	} else {
		return customStatus
	}
}

func (sm *WhitelabelShardManager) ListenStatusUpdates() {
	ch := make(chan uint64)
	go statusupdates.Listen(sm.redisClient, ch)
	for botId := range ch {
		sm.botsLock.RLock()
		shard, ok := sm.bots[botId]
		sm.botsLock.RUnlock()

		if ok {
			shard.UpdateStatus(user.BuildStatus(user.ActivityTypePlaying, sm.getStatus(botId)))
		}
	}
}

func (sm *WhitelabelShardManager) ListenDelete() {
	ch := make(chan uint64)
	go whitelabeldelete.Listen(sm.redisClient, ch)
	for botId := range ch {
		sm.botsLock.RLock()
		shard, ok := sm.bots[botId]
		sm.botsLock.RUnlock()

		if ok {
			shard.Kill() // TODO: Sentry

			sm.botsLock.Lock()
			delete(sm.bots, botId)
			sm.botsLock.Unlock()
		}
	}
}
