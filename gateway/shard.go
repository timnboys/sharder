package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TicketsBot/common/eventforwarding"
	"github.com/rxdn/gdl/cache"
	"github.com/rxdn/gdl/gateway/payloads"
	"github.com/rxdn/gdl/gateway/payloads/events"
	"github.com/rxdn/gdl/objects/user"
	"github.com/rxdn/gdl/rest/ratelimit"
	"github.com/rxdn/gdl/utils"
	"github.com/sirupsen/logrus"
	"github.com/tatsuworks/czlib"
	"log"
	"net/http"
	"nhooyr.io/websocket"
	"sync"
	"time"
)

type Shard struct {
	ShardManager ShardManager
	RateLimiter  *ratelimit.Ratelimiter
	Options      ShardOptions
	Token        string
	ShardId      int

	state     State
	stateLock sync.RWMutex

	WebSocket  *websocket.Conn
	context    context.Context
	zLibReader wrappedReader
	readLock   sync.Mutex

	sequenceLock   sync.RWMutex
	sequenceNumber *int

	lastHeartbeat     int64 // Millis
	heartbeatInterval int
	hasDoneHeartbeat  bool

	lastHeartbeatAcknowledgement int64 // Millis
	heartbeatLock                sync.RWMutex
	killHeartbeat                chan struct{}

	sessionId string

	Cache *cache.PgCache

	// tickets
	selfId uint64
}

func NewShard(shardManager ShardManager, token string, shardId int, rateLimiter *ratelimit.Ratelimiter, options ShardOptions) Shard {
	return Shard{
		ShardManager:                 shardManager,
		RateLimiter:                  rateLimiter,
		Options:                      options,
		Token:                        token,
		ShardId:                      shardId,
		state:                        DEAD,
		context:                      context.Background(),
		lastHeartbeatAcknowledgement: utils.GetCurrentTimeMillis(),
		Cache:                        shardManager.getCache(),
	}
}

func (s *Shard) EnsureConnect() {
	if err := s.Connect(); err != nil {
		logrus.Warnf("shard %d: Error whilst connecting: %s", s.ShardId, err.Error())

		// filter out errors we should delete the token for
		if errors.Is(err, ErrAuthenticationFailed) || errors.Is(err, ErrDisallowedIntents) || errors.Is(err, ErrShardingRequired) {
			s.ShardManager.onFatalError(s.Token, err)
			return
		}

		time.Sleep(500 * time.Millisecond)
		s.EnsureConnect()
	}
}

func (s *Shard) Connect() error {
	logrus.Infof("shard %d: Starting", s.ShardId)

	// Connect to Discord
	s.stateLock.Lock() // Can't RLock - potential state issue
	state := s.state

	if state != DEAD {
		s.stateLock.Unlock()
		if err := s.Kill(); err != nil {
			return err
		}
		s.stateLock.Lock()
	}

	s.state = CONNECTING
	s.stateLock.Unlock()

	// initialise zlib reader
	zlibReader, err := czlib.NewReader(bytes.NewReader(nil))
	if err != nil {
		return err
	}

	s.zLibReader = wrappedReader{
		ReadCloser: zlibReader,
		closeChan:  make(chan struct{}),
	}
	defer zlibReader.Close()

	headers := http.Header{}
	headers.Add("accept-encoding", "zlib")

	conn, _, err := websocket.Dial(s.context, "wss://gateway.discord.gg/?v=6&encoding=json&compress=zlib-stream", &websocket.DialOptions{
		CompressionMode: websocket.CompressionContextTakeover,
		HTTPHeader:      headers,
	})

	if err != nil {
		s.stateLock.Lock()
		s.state = DEAD
		s.stateLock.Unlock()
		return err
	}

	conn.SetReadLimit(4294967296)

	s.WebSocket = conn

	// Read hello
	if err := s.read(); err != nil {
		logrus.Warnf("shard %d: Error whilst reading Hello: %s", s.ShardId, err.Error())
		s.Kill()

		if statusCode := websocket.CloseStatus(err); statusCode != 1 {
			if gatewayError, found := Errors[int(statusCode)]; found {
				return gatewayError
			}
		}

		return err
	}

	if s.sessionId == "" || s.sequenceNumber == nil {
		s.identify()
	} else {
		s.resume()
	}

	logrus.Infof("shard %d: Connected", s.ShardId)

	s.stateLock.Lock()
	s.state = CONNECTED
	s.stateLock.Unlock()

	// read READY to check for auth error etc
	if err := s.read(); err != nil {
		logrus.Warnf("shard %d: Error whilst reading Ready: %s", s.ShardId, err.Error())
		s.Kill()

		if statusCode := websocket.CloseStatus(err); statusCode != 1 {
			if gatewayError, found := Errors[int(statusCode)]; found {
				return gatewayError
			}
		}

		return err
	}

	go func() {
		for {
			// Verify that we are still connected
			s.stateLock.RLock()
			state := s.state
			s.stateLock.RUnlock()
			if state != CONNECTED {
				break
			}

			// Read
			if err := s.read(); err != nil {
				logrus.Warnf("shard %d: Error whilst reading payload: %s", s.ShardId, err.Error())

				s.stateLock.Lock()
				state := s.state
				s.stateLock.Unlock()

				if state == CONNECTED {
					s.Kill()
					s.EnsureConnect()
				}
			}
		}
	}()

	return nil
}

func (s *Shard) identify() {
	// build payload
	identify := payloads.NewIdentify(
		s.ShardId,
		s.Options.ShardCount.Total,
		s.Token,
		s.Options.Presence,
		s.Options.GuildSubscriptions,
		s.Options.Intents...,
	)

	// wait for ratelimit
	if err := s.RateLimiter.IdentifyWait(s.ShardId); err != nil {
		logrus.Warnf("shard %d: Error whilst waiting on identify ratelimit: %s", s.ShardId, err.Error())
	}

	if err := s.write(identify); err != nil {
		logrus.Warnf("shard %d: Error whilst sending Identify: %s", s.ShardId, err.Error())
		s.identify()
	}
}

func (s *Shard) resume() {
	s.sequenceLock.RLock()
	resume := payloads.NewResume(s.Token, s.sessionId, *s.sequenceNumber)
	s.sequenceLock.RUnlock()

	logrus.Infof("shard %d: Resuming", s.ShardId)

	if err := s.write(resume); err != nil {
		logrus.Warnf("shard %d: Error whilst sending Resume: %s", s.ShardId, err.Error())
		s.identify()
	}
}

func (s *Shard) read() error {
	defer func() {
		if r := recover(); r != nil {
			logrus.Warnf("Recovered panic while reading: %s", r)
			s.Kill()
			go s.EnsureConnect()
		}
	}()

	data, err := s.readData()
	if err != nil {
		return err
	}

	payload, err := payloads.NewPayload(data)

	// Handle new sequence number
	if payload.SequenceNumber != nil {
		s.sequenceLock.Lock()
		s.sequenceNumber = payload.SequenceNumber
		s.sequenceLock.Unlock()
	}

	// Handle payload
	switch payload.Opcode {
	case 0: // Event
		{
			go func() {
				eventType := events.EventType(payload.EventName)
				event := s.ExecuteEvent(eventType, payload.Data) // cache data

				// apply extra field
				var extra eventforwarding.Extra
				if event != nil {
					if guildCreate, ok := event.(WrappedGuildCreate); ok {
						extra.IsJoin = guildCreate.IsJoin
					}
				}

				// forward event to workers
				eventforwarding.ForwardEvent(s.ShardManager.getRedis(), eventforwarding.Event{
					BotToken:     s.Token,
					BotId:        s.selfId,
					IsWhitelabel: s.ShardManager.IsWhitelabel(),
					ShardId:      s.ShardId,
					EventType:    payload.EventName,
					Data:         payload.Data,
					Extra:        extra,
				})
			}()
		}
	case 7: // Reconnect
		{
			logrus.Infof("shard %d: received reconnect payload from discord", s.ShardId)

			s.Kill()
			go s.EnsureConnect()
		}
	case 9: // Invalid session
		{
			logrus.Infof("shard %d: received invalid session payload from discord", s.ShardId)
			s.Kill()
			s.sessionId = ""
			go s.EnsureConnect()
		}
	case 10: // Hello
		{
			hello, err := payloads.NewHello(data)
			if err != nil {
				return err
			}

			s.heartbeatInterval = hello.EventData.Interval
			s.killHeartbeat = make(chan struct{})

			ticker := time.NewTicker(time.Duration(int32(s.heartbeatInterval)) * time.Millisecond)
			go s.CountdownHeartbeat(ticker)
		}
	case 11: // Heartbeat ACK
		{
			_, err := payloads.NewHeartbeackAck(data)
			if err != nil {
				log.Println(err.Error())
				return err
			}

			s.heartbeatLock.Lock()
			s.lastHeartbeatAcknowledgement = utils.GetCurrentTimeMillis()
			s.heartbeatLock.Unlock()
		}
	}

	return nil
}

func (s *Shard) readData() ([]byte, error) {
	s.readLock.Lock()
	defer s.readLock.Unlock()

	if s.WebSocket == nil {
		return nil, errors.New("websocket is nil")
	}

	_, reader, err := s.WebSocket.Reader(context.Background())
	if err != nil {
		return nil, err
	}

	// decompress
	s.zLibReader.Reset(reader)
	data, err := s.zLibReader.Read()

	return data, err
}

func (s *Shard) write(payload interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return s.writeRaw(encoded)
}

func (s *Shard) writeRaw(data []byte) error {
	if s.WebSocket == nil {
		msg := fmt.Sprintf("shard %d: WS is closed", s.ShardId)
		logrus.Warn(msg)
		return errors.New(msg)
	}

	err := s.WebSocket.Write(s.context, websocket.MessageText, data)

	return err
}

func (s *Shard) Kill() error {
	logrus.Infof("killing shard %d", s.ShardId)

	go func() {
		s.killHeartbeat <- struct{}{}
	}()

	if err := s.zLibReader.Close(); err != nil {
		logrus.Warnf("shard %d: error closing zlib: %s", s.ShardId, err.Error())
	}

	s.stateLock.Lock()
	s.state = DISCONNECTING

	var err error
	if s.WebSocket != nil {
		err = s.WebSocket.Close(4000, "unknown")
	}

	s.WebSocket = nil

	s.state = DEAD
	s.stateLock.Unlock()

	logrus.Infof("killed shard %d", s.ShardId)

	return err
}

func (s *Shard) UpdateStatus(data user.UpdateStatus) error {
	return s.write(payloads.NewPresenceUpdate(data))
}
