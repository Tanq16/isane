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

  compose network:
        postgres        (no published port, the app reaches it by service name)
        127.0.0.1:6379  redis      (livekit and egress reach each other through it)

  host network:
        livekit         (binds the host directly, so 7880 is closed by the firewall)
        egress          (no ports, connects out to livekit and redis)

  shared volumes:
        ./data/postgres          -> postgres data
        ./data/media             -> app: uploads, thumbnails
        ./data/media/recordings  -> app and egress: call recordings
```

LiveKit and egress run on the host network, which is what LiveKit's own documentation asks for. Publishing three hundred UDP ports instead spawns one `docker-proxy` process per port unless the Docker daemon is reconfigured host-wide, and that reconfiguration restarts every other container on the machine.

The consequence is that LiveKit's HTTP API binds `0.0.0.0:7880` whether or not you want it to, and the firewall is the only thing keeping it private. Caddy reaches it over loopback.

Every other container port binds to `127.0.0.1`, except LiveKit's media, ICE/TCP, and TURN ports, which clients must reach directly.

## Host prerequisites

- Docker Engine with the Compose plugin.
- Caddy installed as a host binary, with a DNS A record already pointing at the VPS.

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

A cloud VPS has a second firewall in front of the host one. The provider's security list or network ACL drops what it does not allow before `ufw` ever sees the packet, so the same four rules have to be added there too, sourced from `0.0.0.0/0`, on the security list attached to the subnet the instance actually uses. Use the ports in your own `livekit.yaml` rather than the ones above if you moved `rtc.tcp_port` or the `rtc` and `turn` port ranges, which is what a second LiveKit already holding the defaults on the host forces you to do.

Verify the rules from outside the host, not from it: a TCP connection to the ICE/TCP port must be accepted, and a STUN binding request to the public address on `3478/udp` must be answered. Signaling that works while every call stays silent is almost always a missing rule here, because signaling arrives over 443 through Caddy and media does not.

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

Create `.env` beside `compose.yaml`. It holds the Postgres password, which compose reads into both the database and the application's `ISANE_DATABASE_URL`, and this host's public IP, which compose passes to LiveKit as `NODE_IP`:

```
POSTGRES_PASSWORD=<the first generated secret>
PUBLIC_IP=<the address your domain resolves to>
```

`APP_PORT` is the one other variable compose reads, defaulting to `8080`. Set it when something else on the host already holds that port; the container still listens on 8080 and only the loopback publish moves. Two other files follow it: the `reverse_proxy` address in `Caddyfile`, and `webhook.urls` in `livekit.yaml`, because LiveKit runs on the host network and reaches the application through that published loopback port. A webhook left pointing at the old port costs participant join and leave tracking, call end, and recording finalization, while signaling and media keep working, so the stack looks healthy until somebody leaves a call.

`PUBLIC_IP` is the address LiveKit advertises to clients as its ICE and TURN candidate, so it must be the public address your domain resolves to rather than the host's private interface. Compose refuses to start without it. Setting it is also what keeps the deployment off third-party infrastructure: LiveKit's `use_external_ip` discovers the address by querying `global.stun.twilio.com` and `stun.l.google.com`, and supplying the address directly skips that lookup. `NODE_IP` overrides `rtc.node_ip` in `livekit.yaml`, so either place works and the environment wins.

`rtc.ips.includes` in `livekit.yaml` narrows the host addresses LiveKit offers as ICE candidates to the listed CIDRs. LiveKit runs on the host network, so without it every Docker bridge on the machine is enumerated and offered, including the bridges of unrelated stacks, and a browser wastes pairing time against an address that may also exist on its own LAN. Set it to the subnet of this host's private interface, which is the one internal address egress needs. Interface excludes are the wrong lever, because compose names its bridges `br-<hash>` and those names change.

In `config.yaml`, set `server.public_url` and `livekit.public_url` to the real hostname, both VAPID keys, `push.subject` to a `mailto:` address you own, and `livekit.api_secret`. Leave `database.url` as it is, since compose overrides it.

In `livekit.yaml` and `egress.yaml`, replace both `REPLACE_ME_openssl_rand_base64_32` values with the same LiveKit API secret you put in `config.yaml`. Leave `rtc.node_ip` commented out unless you are running LiveKit outside compose, since compose supplies it from `PUBLIC_IP`.

**4. Create the data directories with the right ownership.**

The application runs as UID 10001. The egress container runs as its own user in group 0, so the recordings directory is group-writable and setgid, which is what lets egress write files the application can later sweep. Recordings are written flat into that directory, because a subdirectory egress created would carry egress's own ownership and the application could not unlink from it.

```bash
mkdir -p data/postgres data/media/recordings
sudo chown -R 10001:10001 data/media
sudo chown 10001:0 data/media/recordings
sudo chmod 2775 data/media/recordings
sudo chown 10001:10001 config.yaml && sudo chmod 600 config.yaml
```

`config.yaml` needs that ownership too. It is mounted read-only into the application container, which reads it as UID 10001, so a file left at mode 600 owned by the deploying user crash-loops the container with `permission denied` before anything else starts.

**5. Start the stack.**

```bash
docker compose up -d
```

**6. Trust the proxy.**

The application runs in a container and Caddy runs on the host, so the peer address of every request is the compose bridge gateway. Until `server.trusted_proxies` names that bridge, every session row and every audit event records the gateway instead of the client. The subnet only exists once the stack has started, which is why this follows step 5:

```bash
docker network inspect isane_default -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}'
```

Put that CIDR in `server.trusted_proxies` as a single-entry list, then `sudo chown 10001:10001 config.yaml && sudo chmod 600 config.yaml && docker compose restart app`. Leaving the list empty is safe rather than wrong, since an empty list ignores `X-Forwarded-For` entirely, but the addresses it records are useless.

**7. Point Caddy at it.**

Copy `Caddyfile.example` into the host Caddy configuration, replace `chat.example.com` with the real hostname, and reload Caddy. Caddy v2 proxies WebSocket upgrades through `reverse_proxy` with no extra directive, so both the application socket at `/ws` and LiveKit signaling under `/livekit` traverse it.

**8. Claim the first account.**

```bash
docker compose logs app | grep invite
```

The first start against an empty `users` table prints a one-time invite URL. Opening it creates the first account with `is_admin` set. The URL is not persisted anywhere else, so a lost one means dropping the `users` table and restarting.

## Configuration

`config.yaml` is read at start. Every scalar value may be overridden by an environment variable named `ISANE_` followed by the YAML path uppercased and joined with underscores, so `push.vapid_private_key` becomes `ISANE_PUSH_VAPID_PRIVATE_KEY`. A list of strings takes the same form with its entries separated by commas. The `media_quality.video.simulcast_layers` list is the one value with no environment form.

| Key | Default | Holds |
|---|---|---|
| `server.bind` | `0.0.0.0:8080` | Address the HTTP server listens on |
| `server.public_url` | `http://localhost:8080` | Origin the browser reaches. Every write is rejected with 403 when the browser's `Origin` does not match it exactly, so an alias hostname or a second port needs its own deployment |
| `server.insecure` | `false` | Development mode, which disables push because push needs a trusted certificate |
| `server.trusted_proxies` | `[]` | List of CIDR blocks, or bare IP addresses, whose `X-Forwarded-For` header is believed. An empty list ignores the header entirely and records the peer address instead. Running the application in Docker behind a reverse proxy on the host makes the Docker bridge gateway the peer, so the bridge subnet is what has to be trusted |
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
| `retention.staged_upload_hours` | `24` | Hours before an upload that never reached a message is swept, unless it is in use as a user's avatar |
| `retention.audit_days` | `90` | Days before an audit event is deleted, where 0 keeps them forever |
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

A daily job runs four sweeps in order:

1. Staged attachments older than `retention.staged_upload_hours` with no message, deleted along with their files. An attachment in use as a user's avatar is exempt and is swept once it is replaced.
2. Recordings older than `retention.recording_days`, deleted along with their `call_recordings` rows.
3. Audit events older than `retention.audit_days`, deleted from `audit_events`.
4. If `retention.message_days` is non-zero, messages older than that are hard-deleted and the attachments orphaned by it are swept.

`retention.message_days: 0` disables message deletion and is the default. Deleting messages leaves `seq` gaps, which clients tolerate because they page by cursor rather than counting.

The admin interface shows the configured policy and when the sweep last ran.

## Diagnosing a silent agent

Run each step in order and stop at the first one that explains the silence. `psql` below is `docker compose exec postgres psql -U chat -d chat`.

1. **Check `agent_jobs` for a row.** No row means the mention was never extracted, so check the `mentions` table for the triggering message.
2. **A row stuck in `queued` means no daemon is polling.** Check `agents.last_seen_at` for that handle. A daemon that has never registered leaves the agent in `reserved`.
3. **A row stuck in `dispatched` means the daemon took the job and did not answer.** The server holds nothing more; the reason is in the daemon's own logs on the owner's machine.
4. **A row in `failed` or `timeout` carries the reason in `error`.** The same text was already posted into the channel, so the user has seen it too.

## Diagnosing a missing notification

1. **Confirm the user has a `push_subscriptions` row with `enabled = true`.** No row means either that permission was never granted, or that the browser dropped the subscription it had. Reopening the app re-registers it whenever permission is already granted.
2. **Confirm the device was not looking at the page.** A device whose tab is connected and visible is suppressed by design and renders the notification in the page instead. Suppression is per device, so another device left open never silences this one.
3. **Run the routing predicate by hand** against the user, the message, and the container. A channel left at its `mentions` default, carrying a message with no mention, is working correctly.
4. **Check the last send's status.** A 404 or 410 means the subscription is dead and the row should already have been deleted.

Push does not work at all on iOS from a Safari tab. The site has to be added to the home screen first, and a subscription made from a tab is not a defect to chase.
