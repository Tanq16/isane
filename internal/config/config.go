package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       Server       `yaml:"server"`
	Database     Database     `yaml:"database"`
	Push         Push         `yaml:"push"`
	LiveKit      LiveKit      `yaml:"livekit"`
	Media        Media        `yaml:"media"`
	Retention    Retention    `yaml:"retention"`
	Agents       Agents       `yaml:"agents"`
	MediaQuality MediaQuality `yaml:"media_quality"`
}

type Server struct {
	Bind      string `yaml:"bind"`
	PublicURL string `yaml:"public_url"`
	Insecure  bool   `yaml:"insecure"`
}

type Database struct {
	URL string `yaml:"url"`
}

type Push struct {
	VAPIDPublicKey  string `yaml:"vapid_public_key"`
	VAPIDPrivateKey string `yaml:"vapid_private_key"`
	Subject         string `yaml:"subject"`
}

type LiveKit struct {
	PublicURL   string `yaml:"public_url"`
	InternalURL string `yaml:"internal_url"`
	APIKey      string `yaml:"api_key"`
	APISecret   string `yaml:"api_secret"`
}

type Media struct {
	Root              string `yaml:"root"`
	MaxUploadBytes    int64  `yaml:"max_upload_bytes"`
	ImageMaxDimension int    `yaml:"image_max_dimension"`
}

type Retention struct {
	MessageDays       int `yaml:"message_days"`
	RecordingDays     int `yaml:"recording_days"`
	StagedUploadHours int `yaml:"staged_upload_hours"`
}

type Agents struct {
	JobTimeout time.Duration `yaml:"job_timeout"`
}

type MediaQuality struct {
	Video       VideoQuality  `yaml:"video"`
	ScreenShare ScreenQuality `yaml:"screen_share"`
	Audio       AudioQuality  `yaml:"audio"`
	Codec       string        `yaml:"codec"`
}

type VideoQuality struct {
	MaxResolution   int              `yaml:"max_resolution" json:"max_resolution"`
	MaxBitrate      int              `yaml:"max_bitrate" json:"max_bitrate"`
	MaxFramerate    int              `yaml:"max_framerate" json:"max_framerate"`
	SimulcastLayers []SimulcastLayer `yaml:"simulcast_layers" json:"simulcast_layers"`
}

type SimulcastLayer struct {
	Height  int `yaml:"height" json:"height"`
	Bitrate int `yaml:"bitrate" json:"bitrate"`
}

type ScreenQuality struct {
	MaxResolution int `yaml:"max_resolution" json:"max_resolution"`
	MaxBitrate    int `yaml:"max_bitrate" json:"max_bitrate"`
	MaxFramerate  int `yaml:"max_framerate" json:"max_framerate"`
}

type AudioQuality struct {
	MaxBitrate int  `yaml:"max_bitrate" json:"max_bitrate"`
	DTX        bool `yaml:"dtx" json:"dtx"`
}

func Default() Config {
	return Config{
		Server: Server{
			Bind:      "0.0.0.0:8080",
			PublicURL: "http://localhost:8080",
		},
		Media: Media{
			Root:              "/media",
			MaxUploadBytes:    104857600,
			ImageMaxDimension: 2560,
		},
		Retention: Retention{
			MessageDays:       0,
			RecordingDays:     90,
			StagedUploadHours: 24,
		},
		Agents: Agents{JobTimeout: 5 * time.Minute},
		MediaQuality: MediaQuality{
			Video: VideoQuality{
				MaxResolution: 720,
				MaxBitrate:    1700000,
				MaxFramerate:  30,
				SimulcastLayers: []SimulcastLayer{
					{Height: 180, Bitrate: 160000},
					{Height: 360, Bitrate: 450000},
				},
			},
			ScreenShare: ScreenQuality{MaxResolution: 1080, MaxBitrate: 2500000, MaxFramerate: 15},
			Audio:       AudioQuality{MaxBitrate: 48000, DTX: true},
			Codec:       "vp8",
		},
	}
}

// Load reads the YAML file at path over the defaults, then applies environment
// overrides named by uppercasing each YAML path and joining the segments with
// underscores, so push.vapid_private_key is PUSH_VAPID_PRIVATE_KEY.
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err == nil {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := applyEnv(reflect.ValueOf(&cfg).Elem(), nil); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	if c.Server.PublicURL == "" {
		return fmt.Errorf("server.public_url is required")
	}
	if c.Media.Root == "" {
		return fmt.Errorf("media.root is required")
	}
	return nil
}

// PushEnabled reports whether Web Push can be delivered. Push needs a trusted
// certificate, so insecure mode never has working subscriptions.
func (c Config) PushEnabled() bool {
	return !c.Server.Insecure && c.Push.VAPIDPublicKey != "" && c.Push.VAPIDPrivateKey != ""
}

func (c Config) CallsEnabled() bool {
	return c.LiveKit.APIKey != "" && c.LiveKit.APISecret != "" && c.LiveKit.PublicURL != ""
}

func applyEnv(v reflect.Value, path []string) error {
	t := v.Type()
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		field := v.Field(i)
		next := append(append([]string{}, path...), tag)
		if field.Kind() == reflect.Struct && field.Type() != reflect.TypeFor[time.Duration]() {
			if err := applyEnv(field, next); err != nil {
				return err
			}
			continue
		}
		name := strings.ToUpper(strings.Join(next, "_"))
		raw, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := setField(field, raw); err != nil {
			return fmt.Errorf("env %s: %w", name, err)
		}
	}
	return nil
}

func setField(field reflect.Value, raw string) error {
	if field.Type() == reflect.TypeFor[time.Duration]() {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		field.SetInt(int64(d))
		return nil
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	default:
		return fmt.Errorf("unsupported kind %s", field.Kind())
	}
	return nil
}
