package main

import "github.com/TicketsBot/sharder/gateway"

func main() {
	gateway.OverrideEvents()

	manager, err := gateway.NewWhitelabelShardManager()
	if err != nil {
		panic(err)
	}

	go manager.ListenNewTokens()

	if err := manager.Connect(); err != nil {
		panic(err)
	}

	go manager.ListenStatusUpdates()

	gateway.WaitForInterrupt()
}
