package gateway

import "github.com/rxdn/gdl/gateway/payloads/events"

type WrappedGuildCreate struct {
	events.GuildCreate
	IsJoin bool
}
