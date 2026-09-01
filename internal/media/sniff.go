package media

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/tanq16/isane/internal/store"
)

type mediaType struct {
	kind store.AttachmentKind
	mime string
}

type magic struct {
	prefix string
	mediaType
}

var magics = []magic{
	{"II*\x00", mediaType{store.AttachmentImage, "image/tiff"}},
	{"MM\x00*", mediaType{store.AttachmentImage, "image/tiff"}},
	{"fLaC", mediaType{store.AttachmentAudio, "audio/flac"}},
	{"\xff\xfb", mediaType{store.AttachmentAudio, "audio/mpeg"}},
	{"\xff\xf3", mediaType{store.AttachmentAudio, "audio/mpeg"}},
	{"\xff\xf2", mediaType{store.AttachmentAudio, "audio/mpeg"}},
	{"\xff\xf1", mediaType{store.AttachmentAudio, "audio/aac"}},
	{"\xff\xf9", mediaType{store.AttachmentAudio, "audio/aac"}},
}

var ftypBrands = map[string]mediaType{
	"heic": {store.AttachmentImage, "image/heic"},
	"heix": {store.AttachmentImage, "image/heic"},
	"heim": {store.AttachmentImage, "image/heic"},
	"heis": {store.AttachmentImage, "image/heic"},
	"hevc": {store.AttachmentImage, "image/heic"},
	"hevx": {store.AttachmentImage, "image/heic"},
	"mif1": {store.AttachmentImage, "image/heif"},
	"msf1": {store.AttachmentImage, "image/heif"},
	"avif": {store.AttachmentImage, "image/avif"},
	"avis": {store.AttachmentImage, "image/avif"},
	"m4a":  {store.AttachmentAudio, "audio/mp4"},
	"m4b":  {store.AttachmentAudio, "audio/mp4"},
	"qt":   {store.AttachmentVideo, "video/quicktime"},
	"isom": {store.AttachmentVideo, "video/mp4"},
	"iso2": {store.AttachmentVideo, "video/mp4"},
	"iso4": {store.AttachmentVideo, "video/mp4"},
	"iso5": {store.AttachmentVideo, "video/mp4"},
	"iso6": {store.AttachmentVideo, "video/mp4"},
	"mp41": {store.AttachmentVideo, "video/mp4"},
	"mp42": {store.AttachmentVideo, "video/mp4"},
	"mmp4": {store.AttachmentVideo, "video/mp4"},
	"avc1": {store.AttachmentVideo, "video/mp4"},
	"dash": {store.AttachmentVideo, "video/mp4"},
	"m4v":  {store.AttachmentVideo, "video/mp4"},
	"3gp4": {store.AttachmentVideo, "video/3gpp"},
	"3gp5": {store.AttachmentVideo, "video/3gpp"},
	"3g2a": {store.AttachmentVideo, "video/3gpp2"},
}

func sniff(head []byte) (store.AttachmentKind, string) {
	if t, ok := sniffContainer(head); ok {
		return t.kind, t.mime
	}
	for _, m := range magics {
		if bytes.HasPrefix(head, []byte(m.prefix)) {
			return m.kind, m.mime
		}
	}
	mime := http.DetectContentType(head)
	if base := mimeBase(mime); base == "application/ogg" {
		return store.AttachmentAudio, "audio/ogg"
	}
	return kindFor(mime), mime
}

func sniffContainer(head []byte) (mediaType, bool) {
	if len(head) >= 12 && string(head[0:4]) == "RIFF" {
		switch string(head[8:12]) {
		case "WEBP":
			return mediaType{store.AttachmentImage, "image/webp"}, true
		case "WAVE":
			return mediaType{store.AttachmentAudio, "audio/wav"}, true
		case "AVI ":
			return mediaType{store.AttachmentVideo, "video/x-msvideo"}, true
		}
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		if t, ok := ftypBrands[normalizeBrand(head[8:12])]; ok {
			return t, true
		}
		for i := 16; i+4 <= len(head) && i < 64; i += 4 {
			if t, ok := ftypBrands[normalizeBrand(head[i:i+4])]; ok {
				return t, true
			}
		}
		return mediaType{store.AttachmentVideo, "video/mp4"}, true
	}
	if bytes.HasPrefix(head, []byte("\x1a\x45\xdf\xa3")) {
		if bytes.Contains(head, []byte("webm")) {
			return mediaType{store.AttachmentVideo, "video/webm"}, true
		}
		return mediaType{store.AttachmentVideo, "video/x-matroska"}, true
	}
	return mediaType{}, false
}

func normalizeBrand(b []byte) string {
	return strings.ToLower(strings.TrimSpace(string(b)))
}

func kindFor(mime string) store.AttachmentKind {
	switch base := mimeBase(mime); {
	case strings.HasPrefix(base, "image/"):
		return store.AttachmentImage
	case strings.HasPrefix(base, "video/"):
		return store.AttachmentVideo
	case strings.HasPrefix(base, "audio/"):
		return store.AttachmentAudio
	default:
		return store.AttachmentFile
	}
}

func mimeBase(mime string) string {
	base, _, _ := strings.Cut(mime, ";")
	return strings.TrimSpace(strings.ToLower(base))
}
