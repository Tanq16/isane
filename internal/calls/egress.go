package calls

import (
	"context"
	"fmt"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
)

func (s *Service) StartTrackEgress(ctx context.Context, roomName, identity, outPath string) (string, error) {
	adminCtx, err := s.authorize(ctx, &auth.VideoGrant{RoomAdmin: true, Room: roomName})
	if err != nil {
		return "", err
	}
	participant, err := s.rooms.GetParticipant(adminCtx, &livekit.RoomParticipantIdentity{Room: roomName, Identity: identity})
	if err != nil {
		return "", fmt.Errorf("get livekit participant %s: %w", identity, err)
	}
	trackID := audioTrackID(participant)
	if trackID == "" {
		return "", fmt.Errorf("participant %s publishes no audio track", identity)
	}
	recordCtx, err := s.authorize(ctx, &auth.VideoGrant{RoomRecord: true})
	if err != nil {
		return "", err
	}
	info, err := s.egress.StartTrackEgress(recordCtx, &livekit.TrackEgressRequest{
		RoomName: roomName,
		TrackId:  trackID,
		Output: &livekit.TrackEgressRequest_File{
			File: &livekit.DirectFileOutput{Filepath: outPath, DisableManifest: true},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start track egress for %s: %w", identity, err)
	}
	s.log.Debug().
		Str("room", roomName).
		Str("identity", identity).
		Str("egress_id", info.EgressId).
		Str("path", outPath).
		Msg("track egress started")
	return info.EgressId, nil
}

func (s *Service) StopEgress(ctx context.Context, egressID string) error {
	ctx, err := s.authorize(ctx, &auth.VideoGrant{RoomRecord: true})
	if err != nil {
		return err
	}
	if _, err := s.egress.StopEgress(ctx, &livekit.StopEgressRequest{EgressId: egressID}); err != nil {
		return fmt.Errorf("stop egress %s: %w", egressID, err)
	}
	return nil
}

func (s *Service) ListEgress(ctx context.Context, roomName string) ([]string, error) {
	ctx, err := s.authorize(ctx, &auth.VideoGrant{RoomRecord: true})
	if err != nil {
		return nil, err
	}
	res, err := s.egress.ListEgress(ctx, &livekit.ListEgressRequest{RoomName: roomName, Active: true})
	if err != nil {
		return nil, fmt.Errorf("list egress for room %s: %w", roomName, err)
	}
	ids := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		ids = append(ids, item.EgressId)
	}
	return ids, nil
}

func audioTrackID(p *livekit.ParticipantInfo) string {
	var fallback string
	for _, t := range p.Tracks {
		if t.Type != livekit.TrackType_AUDIO {
			continue
		}
		if t.Source == livekit.TrackSource_MICROPHONE {
			return t.Sid
		}
		if fallback == "" {
			fallback = t.Sid
		}
	}
	return fallback
}
