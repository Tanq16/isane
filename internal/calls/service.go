package calls

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils/xtwirp"
	"github.com/twitchtv/twirp"

	"github.com/tanq16/isane/internal/config"
)

var ErrDisabled = errors.New("livekit is not configured")

const (
	apiTimeout       = 30 * time.Second
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	headerTimeout    = 30 * time.Second
	idleTimeout      = 90 * time.Second
)

type Service struct {
	cfg     *config.Config
	enabled bool
	keys    auth.KeyProvider
	rooms   livekit.RoomService
	egress  livekit.Egress
}

func New(cfg *config.Config) *Service {
	s := &Service{cfg: cfg, enabled: cfg.CallsEnabled()}
	if !s.enabled {
		return s
	}
	lk := cfg.LiveKit
	apiURL := httpURL(cmp.Or(lk.InternalURL, lk.PublicURL))
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: headerTimeout,
		IdleConnTimeout:       idleTimeout,
	}}
	opts := xtwirp.DefaultClientOptions()
	s.keys = auth.NewSimpleKeyProvider(lk.APIKey, lk.APISecret)
	s.rooms = livekit.NewRoomServiceProtobufClient(apiURL, client, opts...)
	s.egress = livekit.NewEgressProtobufClient(apiURL, client, opts...)
	return s
}

func (s *Service) Enabled() bool { return s.enabled }

func (s *Service) PublicURL() string { return s.cfg.LiveKit.PublicURL }

func (s *Service) RoomExists(ctx context.Context, roomName string) (bool, error) {
	ctx, cancel, err := s.apiContext(ctx, &auth.VideoGrant{RoomList: true})
	if err != nil {
		return false, err
	}
	defer cancel()
	res, err := s.rooms.ListRooms(ctx, &livekit.ListRoomsRequest{Names: []string{roomName}})
	if err != nil {
		return false, fmt.Errorf("list livekit rooms: %w", err)
	}
	return len(res.Rooms) > 0, nil
}

func (s *Service) DeleteRoom(ctx context.Context, roomName string) error {
	ctx, cancel, err := s.apiContext(ctx, &auth.VideoGrant{RoomCreate: true})
	if err != nil {
		return err
	}
	defer cancel()
	if _, err := s.rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: roomName}); err != nil {
		return fmt.Errorf("delete livekit room %s: %w", roomName, err)
	}
	return nil
}

func (s *Service) apiContext(ctx context.Context, grant *auth.VideoGrant) (context.Context, context.CancelFunc, error) {
	if !s.enabled {
		return nil, nil, ErrDisabled
	}
	token, err := auth.NewAccessToken(s.cfg.LiveKit.APIKey, s.cfg.LiveKit.APISecret).SetVideoGrant(grant).ToJWT()
	if err != nil {
		return nil, nil, fmt.Errorf("sign livekit api token: %w", err)
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	ctx, err = twirp.WithHTTPRequestHeaders(ctx, header)
	if err != nil {
		return nil, nil, fmt.Errorf("attach livekit api token: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	return ctx, cancel, nil
}

func httpURL(url string) string {
	if after, ok := strings.CutPrefix(url, "ws"); ok {
		return "http" + after
	}
	return url
}
