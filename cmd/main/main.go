package main

import (
	"fmt"
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
	fmt.Println("real")
	clusterSize, err := strconv.Atoi(os.Getenv("SHARDER_CLUSTER_SIZE"))
	if err != nil {
		panic(err)
	}

	sharderCount, err := strconv.Atoi(os.Getenv("SHARDER_TOTAL"))
	if err != nil {
		panic(err)
	}

	sharderId, err := strconv.Atoi(os.Getenv("SHARDER_ID"))
	if err != nil {
		panic(err)
	}

	count.Lowest = clusterSize * sharderId
	count.Highest = clusterSize * (sharderId + 1)
	count.Total = clusterSize * sharderCount
	return
}
