package gateway

import "errors"

var (
	ErrUnknown              = errors.New("unknown error")
	ErrUnknownOpcode        = errors.New("unknown opcode")
	ErrDecodeError          = errors.New("decode error")
	ErrNotAuthenticated     = errors.New("not authenticated")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrAlreadyAuthenticated = errors.New("already authenticated")
	ErrInvalidSeq           = errors.New("invalid seq")
	ErrRateLimited          = errors.New("rate limited")
	ErrSessionTimedOut      = errors.New("session timed out")
	ErrInvalidShard         = errors.New("invalid shard")
	ErrShardingRequired     = errors.New("sharding required")
	ErrInvalidApiVersion    = errors.New("invalid api version")
	ErrInvalidIntents       = errors.New("invalid intents")
	ErrDisallowedIntents    = errors.New("disallowed intents")

	Errors = map[int]error{
		4000: ErrUnknown,
		4001: ErrUnknownOpcode,
		4002: ErrDecodeError,
		4003: ErrNotAuthenticated,
		4004: ErrAuthenticationFailed,
		4005: ErrAlreadyAuthenticated,
		4007: ErrInvalidSeq,
		4008: ErrRateLimited,
		4009: ErrSessionTimedOut,
		4010: ErrInvalidShard,
		4011: ErrShardingRequired,
		4012: ErrInvalidApiVersion,
		4013: ErrInvalidIntents,
		4014: ErrDisallowedIntents,
	}
)
