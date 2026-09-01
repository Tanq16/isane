# Deployment

One VPS running Docker, with Caddy on the host as a system binary. Caddy terminates TLS and fronts other subdomains besides this one; everything else runs in containers.

## What runs where

```
                     internet
                        |
        +---------------+----------------+-----------------+
        | 443/tcp                        | 7881/tcp        | 3478 + 50000-50300/udp
        v                                v                 v
  caddy (host binary)              livekit ice/tcp    livekit media and turn
        |
        +--> 127.0.0.1:8080   app       (Go binary, embeds the frontend)
        +--> 127.0.0.1:7880   livekit   (signaling and HTTP API)

  container-internal only:
        127.0.0.1:5432  postgres
        127.0.0.1:6379  redis      (livekit and egress reach each other through it)
        egress          (no ports, connects out to livekit and redis)

  shared volumes:
        ./data/postgres          -> postgres data
        ./data/media             -> app: uploads, thumbnails
        ./data/media/recordings  -> app and egress: call recordings
```

Every container port binds to `127.0.0.1` except LiveKit's media, ICE/TCP, and TURN ports, which clients must reach directly.

## Host prerequisites

- Docker Engine with the Compose plugin.
- Caddy installed as a host binary, with a DNS A record already pointing at the VPS.
- `"userland-proxy": false` in `/etc/docker/daemon.json`, followed by `systemctl restart docker`. The default spawns one `docker-proxy` process per published port, and this deployment publishes over three hundred UDP ports.

## Firewall

```bash
ufw allow 443/tcp
ufw allow 7881/tcp
ufw allow 3478/udp
ufw allow 50000:50300/udp
```

| Port | Carries |
|---|---|
| `443/tcp` | Caddy, which fronts both the application and LiveKit signaling |
| `7881/tcp` | LiveKit ICE/TCP, the fallback for a client whose network blocks UDP |
| `3478/udp` | LiveKit's built-in TURN server, the fallback for a client behind symmetric NAT |
| `50000-50200/udp` | LiveKit media |
| `50201-50300/udp` | LiveKit TURN relay allocations |

LiveKit media must reach clients over UDP directly. Restricting to 443 forces every call to relay over TCP, which adds latency and degrades under packet loss.

## Walkthrough

**1. Clone and create the config files.**

```bash
git clone https://github.com/Tanq16/isane.git && cd isane
cp config.example.yaml config.yaml
cp livekit.example.yaml livekit.yaml
cp egress.example.yaml egress.yaml
```

**2. Generate the secrets.**

```bash
openssl rand -base64 32   # Postgres password
openssl rand -base64 32   # LiveKit API secret
```

VAPID keys for Web Push, as the raw base64url values `webpush-go` expects:

```bash
openssl ecparam -genkey -name prime256v1 -noout -out vapid.pem
openssl ec -in vapid.pem -outform DER | tail -c +8 | head -c 32 | base64 | tr '+/' '-_' | tr -d '='
openssl ec -in vapid.pem -pubout -outform DER | tail -c 65 | base64 | tr '+/' '-_' | tr -d '='
rm vapid.pem
```

The first output is `push.vapid_private_key` and the second is `push.vapid_public_key`.

**3. Fill in the config files.**

`.env` holds the Postgres password, which compose reads into both the database and the application's `DATABASE_URL`:

```
POSTGRES_PASSWORD=<the first generated secret>
```

In `config.yaml`, set `server.public_url` and `livekit.public_url` to the real hostname, both VAPID keys, `push.subject` to a `mailto:` address you own, and `livekit.api_secret`. Leave `database.url` as it is, since compose overrides it.

In `livekit.yaml` and `egress.yaml`, replace both `REPLACE_ME_openssl_rand_base64_32` values with the same LiveKit API secret you put in `config.yaml`.

**4. Create the data directories with the right ownership.**

The application runs as UID 10001. The egress container runs as its own user in group 0, so the recordings directory is group-writable and setgid, which is what lets egress write files the application can later sweep.

```bash
mkdir -p data/postgres data/media/recordings
sudo chown -R 10001:10001 data/media
sudo chown 10001:0 data/media/recordings
sudo chmod 2775 data/media/recordings
```

**5. Start the stack.**

```bash
docker compose up -d --build
```

**6. Point Caddy at it.**

Copy `Caddyfile.example` into the host Caddy configuration, replace `chat.example.com` with the real hostname, and reload Caddy. Caddy v2 proxies WebSocket upgrades through `reverse_proxy` with no extra directive, so both the application socket at `/ws` and LiveKit signaling under `/livekit` traverse it.

**7. Claim the first account.**

```bash
docker compose logs app | grep invite
```

The first start against an empty `users` table prints a one-time invite URL. Opening it creates the first account with `is_admin` set. The URL is not persisted anywhere else, so a lost one means dropping the `users` table and restarting.

## Configuration

`config.yaml` is read at start. Every scalar value may be overridden by an environment variable named by uppercasing the YAML path and joining the segments with underscores, so `push.vapid_private_key` becomes `PUSH_VAPID_PRIVATE_KEY`. The `media_quality.video.simulcast_layers` list is the one value with no environment form.

| Key | Default | Holds |
|---|---|---|
| `server.bind` | `0.0.0.0:8080` | Address the HTTP server listens on |
| `server.public_url` | `http://localhost:8080` | Origin the browser reaches, used in invite and notification URLs |
| `server.insecure` | `false` | Development mode, which disables push because push needs a trusted certificate |
| `database.url` | required | Postgres connection string |
| `push.vapid_public_key` | empty | Served to the browser by `GET /api/push/vapid-key` |
| `push.vapid_private_key` | empty | Push stays off while either key is empty |
| `push.subject` | empty | VAPID `sub` claim, which Apple requires to be a `mailto:` or `https:` URI |
| `livekit.public_url` | empty | Signaling URL the browser connects to |
| `livekit.internal_url` | empty | LiveKit HTTP API, reached from the application container |
| `livekit.api_key` | empty | Must match a key in `livekit.yaml` |
| `livekit.api_secret` | empty | Calls stay off while the key, secret, or public URL is empty |
| `media.root` | `/media` | Directory holding uploads, thumbnails, and recordings |
| `media.max_upload_bytes` | `104857600` | Rejection threshold for a single upload |
| `media.image_max_dimension` | `2560` | Longest edge an uploaded image is downscaled to |
| `retention.message_days` | `0` | Days before a message is hard-deleted, where 0 disables it |
| `retention.recording_days` | `90` | Days before a call recording is deleted |
| `retention.staged_upload_hours` | `24` | Hours before an upload that never reached a message is swept |
| `agents.job_timeout` | `5m` | How long a dispatched agent job may run before it is failed |
| `media_quality` | see `config.example.yaml` | Client publish settings, delivered to the browser in the `ready` frame |

`media_quality` lives in the config so a call's cost can be lowered without a rebuild. Screen share is the most expensive thing this deployment does, at roughly 20 Mbps of egress for one person sharing to four viewers, so `media_quality.screen_share.max_bitrate` is the first value to lower if egress ever matters.

`vp8` is the only codec every target browser supports for WebRTC. VP9 and AV1 cut bitrate meaningfully but are absent from Safari, which excludes every iOS client.

## Backups

The complete state is the Postgres database plus `data/media`. Both must be captured together.

```bash
mkdir -p backup
docker compose exec -T postgres pg_dump -Fc -U chat chat | gzip > backup/db-$(date +%F).sql.gz
tar czf backup/media-$(date +%F).tar.gz data/media
```

Run daily from cron on the host and copy the result off the box. Recordings dominate the media size, so apply `retention.recording_days` before sizing the backup target.

Restoring a dump into a fresh database:

```bash
gunzip -c backup/db-2026-09-01.sql.gz | docker compose exec -T postgres pg_restore -U chat -d chat --clean
```

## Retention

A daily job runs three sweeps in order:

1. Staged attachments older than `retention.staged_upload_hours` with no message, deleted along with their files.
2. Recordings older than `retention.recording_days`, deleted along with their `call_recordings` rows.
3. If `retention.message_days` is non-zero, messages older than that are hard-deleted and the attachments orphaned by it are swept.

`retention.message_days: 0` disables message deletion and is the default. Deleting messages leaves `seq` gaps, which clients tolerate because they page by cursor rather than counting.

The admin interface shows the configured policy and when the sweep last ran.

## Diagnosing a silent agent

Run each step in order and stop at the first one that explains the silence. `psql` below is `docker compose exec postgres psql -U chat -d chat`.

1. **Check `agent_jobs` for a row.** No row means the mention was never extracted, so check the `mentions` table for the triggering message.
2. **A row stuck in `queued` means no daemon is polling.** Check `agents.last_seen_at` for that handle. A daemon that has never registered leaves the agent in `reserved`.
3. **A row stuck in `dispatched` means the daemon took the job and did not answer.** The server holds nothing more; the reason is in the daemon's own logs on the owner's machine.
4. **A row in `failed` or `timeout` carries the reason in `error`.** The same text was already posted into the channel, so the user has seen it too.

## Diagnosing a missing notification

1. **Confirm the user has a `push_subscriptions` row with `enabled = true`.** No row means the browser never subscribed, most often because permission was never requested from a click or keystroke.
2. **Confirm the device did not have a live socket.** A device whose tab is connected is suppressed by design and renders the notification in the page instead.
3. **Run the routing predicate by hand** against the user, the message, and the container. A channel left at its `mentions` default, carrying a message with no mention, is working correctly.
4. **Check the last send's status.** A 404 or 410 means the subscription is dead and the row should already have been deleted.

Push does not work at all on iOS from a Safari tab. The site has to be added to the home screen first, and a subscription made from a tab is not a defect to chase.
