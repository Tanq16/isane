package calls

import (
	"context"
	"fmt"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
)

const recordingAudioBitrate = 48

func (s *Service) StartRoomEgress(ctx context.Context, roomName, outPath string) (string, error) {
	recordCtx, err := s.authorize(ctx, &auth.VideoGrant{RoomRecord: true})
	if err != nil {
		return "", err
	}
	info, err := s.egress.StartRoomCompositeEgress(recordCtx, &livekit.RoomCompositeEgressRequest{
		RoomName:  roomName,
		AudioOnly: true,
		FileOutputs: []*livekit.EncodedFileOutput{{
			FileType:        livekit.EncodedFileType_MP4,
			Filepath:        outPath,
			DisableManifest: true,
		}},
		Options: &livekit.RoomCompositeEgressRequest_Advanced{
			Advanced: &livekit.EncodingOptions{
				AudioCodec:   livekit.AudioCodec_AAC,
				AudioBitrate: recordingAudioBitrate,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start room egress for %s: %w", roomName, err)
	}
	s.log.Debug().
		Str("room", roomName).
		Str("egress_id", info.EgressId).
		Str("path", outPath).
		Msg("room egress started")
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
