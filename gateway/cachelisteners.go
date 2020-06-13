package gateway

import (
	"github.com/rxdn/gdl/gateway/payloads/events"
	"github.com/rxdn/gdl/objects/member"
	"github.com/sirupsen/logrus"
	"time"
)

var cacheListeners = []interface{}{
	readyListener,
	channelCreateListener,
	channelUpdateListener,
	channelDeleteListener,
	guildCreateListener,
	guildUpdateListener,
	guildDeleteListener,
	guildEmojisUpdateListeners,
	guildMemberAddListener,
	guildMemberRemoveListener,
	guildMemberUpdateListener,
	guildMembersChunkListener,
	guildRoleCreateListener,
	guildRoleUpdateListener,
	guildRoleDeleteListener,
	userUpdateListener,
	voiceStateUpdateListener,
}

func readyListener(s *Shard, e *events.Ready) {
	logrus.Infof("shard %d: received ready", s.ShardId)

	s.sessionId = e.SessionId
	s.selfId = e.User.Id
}

func channelCreateListener(s *Shard, e *events.ChannelCreate) {
	s.Cache.StoreChannel(e.Channel)
}

func channelUpdateListener(s *Shard, e *events.ChannelUpdate) {
	s.Cache.StoreChannel(e.Channel)
}

func channelDeleteListener(s *Shard, e *events.ChannelDelete) {
	s.Cache.DeleteChannel(e.Channel.Id)
}

func guildCreateListener(s *Shard, e *WrappedGuildCreate) {
	if _, exists := s.Cache.GetGuild(e.Id, false); !exists {
		// don't mass DM everyone on cache purge lol
		// check if bot joined in the last minute
		if e.JoinedAt.Add(time.Minute).After(time.Now()) {
			e.IsJoin = true
		}
	}

	s.Cache.StoreGuild(e.Guild)
}

func guildUpdateListener(s *Shard, e *events.GuildUpdate) {
	s.Cache.StoreGuild(e.Guild)
}

func guildDeleteListener(s *Shard, e *events.GuildDelete) {
	s.Cache.DeleteGuild(e.Id)
}

func guildEmojisUpdateListeners(s *Shard, e *events.GuildEmojisUpdate) {
	for _, emoji := range e.Emojis {
		s.Cache.StoreEmoji(emoji, e.GuildId)
	}
}

func guildMemberAddListener(s *Shard, e *events.GuildMemberAdd) {
	s.Cache.StoreMember(e.Member, e.GuildId)
}

func guildMemberRemoveListener(s *Shard, e *events.GuildMemberRemove) {
	s.Cache.DeleteMember(e.User.Id, e.GuildId)
}

func guildMemberUpdateListener(s *Shard, e *events.GuildMemberUpdate) {
	s.Cache.StoreMember(member.Member{
		User:         e.User,
		Nick:         e.Nick,
		Roles:        e.Roles,
		PremiumSince: e.PremiumSince,
	}, e.GuildId)
}

func guildMembersChunkListener(s *Shard, e *events.GuildMembersChunk) {
	for _, member := range e.Members {
		s.Cache.StoreMember(member, e.GuildId)
	}
}

func guildRoleCreateListener(s *Shard, e *events.GuildRoleCreate) {
	s.Cache.StoreRole(e.Role, e.GuildId)
}

func guildRoleUpdateListener(s *Shard, e *events.GuildRoleUpdate) {
	s.Cache.StoreRole(e.Role, e.GuildId)
}

func guildRoleDeleteListener(s *Shard, e *events.GuildRoleDelete) {
	s.Cache.DeleteRole(e.RoleId)
}

func userUpdateListener(s *Shard, e *events.UserUpdate) {
	s.Cache.StoreUser(e.User)
}

func voiceStateUpdateListener(s *Shard, e *events.VoiceStateUpdate) {
	s.Cache.StoreVoiceState(e.VoiceState)
}
