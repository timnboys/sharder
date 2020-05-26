package gateway

import (
	"github.com/rxdn/gdl/gateway/intents"
	"github.com/rxdn/gdl/objects/user"
)

type ShardOptions struct {
	ShardCount           ShardCount
	GuildSubscriptions   bool
	Presence             user.UpdateStatus
	Intents              []intents.Intent
	LargeShardingBuckets int // defaults to 1. don't touch unless discord tell you to
}

type ShardCount struct {
	Total   int
	Lowest  int // Inclusive
	Highest int // Exclusive
}
