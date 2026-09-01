package calls

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils/protojson"
)

var (
	errWebhookUnsigned   = errors.New("livekit webhook carries no authorization header")
	errWebhookUnknownKey = errors.New("livekit webhook signed with an unknown api key")
	errWebhookChecksum   = errors.New("livekit webhook body does not match its signature")
)

func (s *Service) VerifyWebhook(r *http.Request) (*livekit.WebhookEvent, error) {
	if !s.enabled {
		return nil, ErrDisabled
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read livekit webhook body: %w", err)
	}
	signature := r.Header.Get("Authorization")
	if signature == "" {
		return nil, errWebhookUnsigned
	}
	verifier, err := auth.ParseAPIToken(signature)
	if err != nil {
		return nil, fmt.Errorf("parse livekit webhook token: %w", err)
	}
	secret := s.keys.GetSecret(verifier.APIKey())
	if secret == "" {
		return nil, errWebhookUnknownKey
	}
	_, grants, err := verifier.Verify(secret)
	if err != nil {
		return nil, fmt.Errorf("verify livekit webhook token: %w", err)
	}
	sum := sha256.Sum256(body)
	digest := base64.StdEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(grants.Sha256), []byte(digest)) != 1 {
		return nil, errWebhookChecksum
	}
	event := &livekit.WebhookEvent{}
	if err := protojson.Unmarshal(body, event); err != nil {
		return nil, fmt.Errorf("decode livekit webhook event: %w", err)
	}
	return event, nil
}
