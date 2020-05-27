package main

import (
	"github.com/TicketsBot/sharder/gateway"
	"github.com/rxdn/gdl/gateway/intents"
	"github.com/rxdn/gdl/objects/user"
	"os"
	"strconv"
)

func main() {
	gateway.OverrideEvents()

	manager, err := gateway.NewPublicShardManager(gateway.ShardOptions{
		ShardCount:           buildShardCount(),
		Presence:             user.BuildStatus(user.ActivityTypePlaying, "DM for help | t!help"),
		Intents:              []intents.Intent{
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
	if err != nil {
		panic(err)
	}

	if err := manager.Connect(); err != nil {
		panic(err)
	}

	gateway.WaitForInterrupt()
}

func buildShardCount() (count gateway.ShardCount) {
	var err error

	// parse total
	count.Total, err = strconv.Atoi(os.Getenv("SHARDER_COUNT_TOTAL"))
	if err != nil {
		panic(err)
	}

	// parse highest (exclusive)
	count.Highest, err = strconv.Atoi(os.Getenv("SHARDER_COUNT_HIGHEST"))
	if err != nil {
		panic(err)
	}

	// parse lowest (inclusive)
	count.Lowest, err = strconv.Atoi(os.Getenv("SHARDER_COUNT_LOWEST"))
	if err != nil {
		panic(err)
	}

	return
}
