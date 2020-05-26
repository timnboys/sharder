package gateway

import (
	"context"
	"github.com/TicketsBot/common/tokenchange"
	"github.com/TicketsBot/database"
	"github.com/go-redis/redis"
	"github.com/jackc/pgx/v4/pgxpool"
	"os"
	"strconv"
	"sync"
)

type WhitelabelShardManager struct {
	total, id int

	db    *database.Database
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

	manager.total, err = strconv.Atoi(os.Getenv("SHARDER_TOTAL")); if err != nil {
		return
	}

	manager.id, err = strconv.Atoi(os.Getenv("SHARDER_ID")); if err != nil {
		return
	}

	{
		db, err := pgxpool.Connect(context.Background(), os.Getenv("SHARDER_PG_URI"))
		if err != nil {
			return
		}

		manager.db = database.NewDatabase(db)
	}

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

		shard := NewShard(sm, bot.Token, 0)

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
		if payload.OldId % uint64(sm.total) != uint64(sm.id) {
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
