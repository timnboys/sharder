package gateway

import (
	"github.com/rxdn/gdl/gateway/payloads/events"
	"reflect"
)

func OverrideEvents() {
	events.EventTypes[events.GUILD_CREATE] = reflect.TypeOf(WrappedGuildCreate{})
}
