package calls

import (
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"

	"github.com/tanq16/isane/internal/store"
)

const tokenValidFor = 6 * time.Hour

func (s *Service) Token(roomName string, u store.User) (string, error) {
	if !s.enabled {
		return "", ErrDisabled
	}
	grant := &auth.VideoGrant{RoomJoin: true, Room: roomName}
	grant.SetCanPublish(true)
	grant.SetCanSubscribe(true)
	grant.SetCanPublishData(true)

	token, err := auth.NewAccessToken(s.cfg.LiveKit.APIKey, s.cfg.LiveKit.APISecret).
		SetIdentity(u.ID.String()).
		SetName(u.DisplayName).
		SetValidFor(tokenValidFor).
		SetVideoGrant(grant).
		ToJWT()
	if err != nil {
		return "", fmt.Errorf("sign livekit token for %s: %w", u.Handle, err)
	}
	return token, nil
}
