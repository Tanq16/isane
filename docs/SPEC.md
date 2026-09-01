# Chat platform build specification

A self-hosted team chat, voice, video, and screen-share platform with AI agents as first-class members. Go backend, vanilla web frontend served as a Progressive Web App, Postgres for state, LiveKit for call media, deployed as containers on a single VPS behind a host-level Caddy.

This document is the complete build specification. It assumes no prior context.

## 1. Scope

### Built

- Channels that every human member belongs to implicitly, with no membership table and no per-channel permissions.
- Direct messages and group direct messages with an explicit participant list.
- Threads and inline replies as two distinct mechanisms.
- Markdown message bodies rendered server-side to HTML.
- File, image, audio, and video attachments with server-side transcoding.
- AI agents as user rows that are mentioned like people and answer in the channel.
- Web Push notifications to iOS and Android home-screen web apps and to desktop browsers.
- Audio calls, video calls, and screen share, scoped to a container, through a self-hosted LiveKit SFU.
- Per-speaker call recording written to disk for later transcription.
- An admin interface for users, invites, agents, and retention.
- Full-text search over message bodies.

### Not built

These are deliberate exclusions. Do not add them.

- End-to-end encryption. The operator owns the server and the database, so transport TLS is the security boundary.
- Federation with any other server or protocol.
- Native iOS or Android applications. The PWA is the only mobile client.
- Role-based access control, permission levels, or moderation tooling beyond an admin flag.
- Multi-tenancy. One deployment serves one team.
- Message reactions, custom emoji, presence status messages, or user profiles beyond a display name and avatar.
- Screen share on mobile. `getDisplayMedia` does not exist in any mobile browser, so the control is hidden on small viewports.
- Ringing incoming calls. A call start is a notification, not a ring.
- Unit tests, unless separately requested.

### Platform ceilings to design around

These are properties of the browsers, not of this design. Do not attempt to work around them.

- An iOS home-screen web app cannot hold a call when the app is backgrounded or the screen locks. Outgoing microphone stops. Calls work while the app is foreground.
- Web Push on iOS requires the site to be added to the home screen. It does not work from a Safari tab.
- Safari renders no notification action buttons and no notification images. There is no reply-from-notification.
- Silent push is prohibited. Every push must render a visible notification, or the push subscription is revoked after repeated failures.
- A self-signed certificate cannot deliver Web Push. Service worker registration and push subscription require a trusted certificate, so `-k` mode is for local development only.

## 2. Deployment topology

One VPS. Caddy runs on the host as a system binary and fronts other subdomains besides this one. Everything else runs in containers. Every container port binds to `127.0.0.1` except the LiveKit media range.

```
                     internet
                        |
        +---------------+----------------+
        | 443/tcp                        | 50000-50200/udp
        v                                v
  caddy (host binary)              livekit media
        |
        +--> 127.0.0.1:8080   app       (Go binary, embeds frontend)
        +--> 127.0.0.1:7880   livekit   (signaling and HTTP API)

  container-internal only:
        127.0.0.1:5432  postgres
        127.0.0.1:6379  redis      (required by egress to reach livekit)
        egress          (no ports, connects out to livekit and redis)

  shared volumes:
        ./data/postgres      -> postgres data
        ./data/media         -> app: uploads, thumbnails
        ./data/media/recordings -> app and egress: call recordings
```

### Caddyfile

```
chat.example.com {
    handle /livekit/* {
        uri strip_prefix /livekit
        reverse_proxy 127.0.0.1:7880
    }
    handle {
        reverse_proxy 127.0.0.1:8080
    }
}
```

Caddy v2 proxies WebSocket upgrades through `reverse_proxy` with no extra directive. Both the application WebSocket at `/ws` and LiveKit signaling at `/livekit` traverse it.

### Firewall

Open `443/tcp` and `50000-50200/udp`. LiveKit media must reach clients over UDP directly. Restricting to 443 forces every call to relay over TURN on TCP, which adds latency and degrades under packet loss.

Verify before deploying whether LiveKit's built-in TURN server covers the fallback case for clients behind symmetric NAT, or whether a separate coturn container is needed.

### compose.yaml

```yaml
services:
  app:
    build: .
    restart: unless-stopped
    ports: ["127.0.0.1:8080:8080"]
    volumes:
      - ./data/media:/media
      - ./config.yaml:/config.yaml:ro
    depends_on: [postgres]

  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    ports: ["127.0.0.1:5432:5432"]
    volumes: ["./data/postgres:/var/lib/postgresql/data"]
    environment:
      POSTGRES_DB: chat
      POSTGRES_USER: chat
      POSTGRES_PASSWORD_FILE: /run/secrets/pg_password

  livekit:
    image: livekit/livekit-server
    restart: unless-stopped
    command: --config /livekit.yaml
    ports:
      - "127.0.0.1:7880:7880"
      - "50000-50200:50000-50200/udp"
    volumes: ["./livekit.yaml:/livekit.yaml:ro"]
    depends_on: [redis]

  egress:
    image: livekit/egress
    restart: unless-stopped
    cap_add: [SYS_ADMIN]
    volumes:
      - ./data/media/recordings:/out
      - ./egress.yaml:/egress.yaml:ro
    environment:
      EGRESS_CONFIG_FILE: /egress.yaml
    depends_on: [livekit, redis]

  redis:
    image: redis:alpine
    restart: unless-stopped
    ports: ["127.0.0.1:6379:6379"]
```

`cap_add: SYS_ADMIN` is required by the egress container because Chrome sandboxing is enabled by default in the image. This deployment uses track egress rather than room-composite egress, which does not launch Chrome, but the capability requirement belongs to the image.

`ffmpeg` and `imagemagick` are installed into the `app` image rather than run as a separate service. The application shells out to them.

### config.yaml

```yaml
server:
  bind: 0.0.0.0:8080
  public_url: https://chat.example.com
  insecure: false          # -k mode: self-signed, no push, dev only

database:
  url: postgres://chat:PASSWORD@postgres:5432/chat?sslmode=disable

push:
  vapid_public_key: BN...
  vapid_private_key: ...
  subject: mailto:admin@example.com

livekit:
  public_url: wss://chat.example.com/livekit
  internal_url: http://livekit:7880
  api_key: ...
  api_secret: ...

media:
  root: /media
  max_upload_bytes: 104857600
  image_max_dimension: 2560

retention:
  message_days: 0          # 0 disables message deletion
  recording_days: 90
  staged_upload_hours: 24

agents:
  job_timeout: 5m
```

Secrets are read from the file, and every value may be overridden by an environment variable named by uppercasing the path and joining with underscores, so `push.vapid_private_key` becomes `PUSH_VAPID_PRIVATE_KEY`.

### Media quality settings

These are the client-side publish settings, matching a configuration proven in production by Element Call. They belong in the config file so they can be lowered without a rebuild.

```yaml
media_quality:
  video:
    max_resolution: 720
    max_bitrate: 1700000
    max_framerate: 30
    simulcast_layers:
      - {height: 180, bitrate: 160000}
      - {height: 360, bitrate: 450000}
  screen_share:
    max_resolution: 1080
    max_bitrate: 2500000
    max_framerate: 15
  audio:
    max_bitrate: 48000
    dtx: true
  codec: vp8
```

`vp8` is the only codec every target browser supports for WebRTC. VP9 and AV1 cut bitrate meaningfully but are not available in Safari, which excludes every iOS client.

Screen share is the most expensive thing this deployment does. One person sharing at the top layer to four viewers costs roughly 20 Mbps of egress. `screen_share.max_bitrate` is the first value to lower if egress ever matters.

### Expected egress

With `adaptiveStream` and `dynacast` enabled, a subscriber receives the simulcast layer matched to its rendered tile height, and a publisher stops encoding layers nobody watches. Layer selection thresholds are 200 px and 400 px of rendered tile height.

| Scenario | Sustained egress | Per hour |
|---|---|---|
| 3 people, cameras on, laptop grid | 3.1 Mbps | 1.4 GB |
| 5 people, cameras on, laptop grid | 10.5 Mbps | 4.7 GB |
| 5 people, audio only | 0.96 Mbps | 0.43 GB |
| 3 people, all tiles above 400 px | 10.5 Mbps | 4.7 GB |
| 1 screen share to 4 viewers | 20 Mbps | 9.0 GB |

On a 10 TB monthly allowance none of these binds. CPU does not bind either: an SFU forwards RTP without transcoding, and LiveKit's own default node limit is 400 tracks per CPU against the 20 tracks a ten-person call produces.

## 3. Data model

Postgres 17. Types are written as pseudo-DDL: implement with whatever migration tool the project settles on, embedding the SQL in the binary.

### Identity

```sql
users
  id              uuid    primary key
  kind            enum('human','agent')  not null
  handle          text    not null unique   -- ^[a-z0-9][a-z0-9_-]{0,31}$
  display_name    text    not null
  avatar_id       uuid    null references attachments(id)
  email           text    null unique       -- humans only
  password_hash   text    null              -- humans only, argon2id
  is_admin        boolean not null default false
  created_at      timestamptz not null
  deactivated_at  timestamptz null
```

A handle is permanent and never reused, because message authorship and mention history point at it. Deactivating a human keeps the row and clears `password_hash`. An agent has no `email` and no `password_hash`.

```sql
sessions
  id            uuid primary key
  token_hash    bytea not null unique      -- sha256 of the cookie value
  user_id       uuid not null references users(id)
  created_at    timestamptz not null
  last_seen_at  timestamptz not null
  expires_at    timestamptz not null
  user_agent    text
  ip            inet

invites
  token_hash    bytea primary key
  created_by    uuid not null references users(id)
  note          text
  expires_at    timestamptz not null
  used_by       uuid null references users(id)
  used_at       timestamptz null
```

The session cookie carries 32 random bytes, base64url-encoded. Only its SHA-256 is stored, so a database dump yields no live sessions.

### Containers

A container holds messages. There are two kinds, and they differ only in how membership is determined.

```sql
containers
  id           uuid primary key
  kind         enum('channel','conversation') not null
  slug         text null unique       -- channels only, ^[a-z0-9][a-z0-9-]{0,63}$
  name         text null              -- channels only
  topic        text null
  last_seq     bigint not null default 0
  created_by   uuid not null references users(id)
  created_at   timestamptz not null
  archived_at  timestamptz null

conversation_participants
  container_id uuid not null references containers(id)
  user_id      uuid not null references users(id)
  added_at     timestamptz not null
  primary key (container_id, user_id)
```

Membership resolution:

```
members(container):
    if container.kind == 'channel':
        return all users where kind = 'human' and deactivated_at is null
    else:
        return conversation_participants for container
```

Channels have no membership rows. Every active human sees every channel. This is the single largest simplification in the design and it eliminates joins, invitations, permission checks, and the entire access-control surface. Do not reintroduce it.

A conversation is created by its participant set. Creating a conversation with an existing participant set returns the existing container rather than a duplicate, keyed on the sorted participant id list.

An agent is never a `conversation_participant` and is never counted in `members()`. Agents receive work only through mentions.

### Messages

```sql
messages
  id                    uuid primary key
  container_id          uuid   not null references containers(id)
  seq                   bigint not null
  author_id             uuid   not null references users(id)
  body                  text   not null        -- markdown, the only stored form
  reply_to_id           uuid   null references messages(id)
  thread_root_id        uuid   null references messages(id)
  client_id             text   not null
  thread_reply_count    int    not null default 0
  thread_last_reply_at  timestamptz null
  search_tsv            tsvector generated always as (to_tsvector('english', body)) stored
  created_at            timestamptz not null
  edited_at             timestamptz null
  deleted_at            timestamptz null

  unique (container_id, seq)
  unique (author_id, client_id)
  index on (container_id, seq desc) where thread_root_id is null
  index on (thread_root_id, seq)
  gin index on search_tsv
```

**`seq` is the backbone of the whole system.** It is a monotonic per-container integer, allocated inside the insert transaction:

```
begin
  update containers set last_seq = last_seq + 1
    where id = $container returning last_seq   -- row lock serialises allocation
  insert into messages (..., seq) values (..., $last_seq)
commit
```

Never order, page, or resume by timestamp. Client clocks skew, server clocks jump under NTP correction, and two messages in the same millisecond have no defined order. Every read cursor, every unread count, and every reconnect gap is expressed in `seq`.

`client_id` is generated by the sending client. The unique constraint on `(author_id, client_id)` makes a retried send idempotent: a client that sends, loses its connection before the acknowledgement, and retries gets the original message back rather than a duplicate.

**Threads and replies are different mechanisms and both are needed.**

- `reply_to_id` is an inline quote. The message stays in the main timeline and renders with a quoted excerpt of its parent above it.
- `thread_root_id` moves the message out of the main timeline into a thread pane rooted at that message. The root message itself has `thread_root_id = null`.

A message may carry both. The main timeline query filters `thread_root_id is null`. A thread query filters on `thread_root_id = $root`. `thread_reply_count` and `thread_last_reply_at` are denormalized onto the root so the timeline can render a thread summary without a subquery per message.

Editing rewrites `body` and stamps `edited_at`. It does not change `seq`. Deleting sets `deleted_at` and blanks `body`; the row survives so `seq` stays gapless and clients holding a cursor do not see a hole.

### Mentions

```sql
mentions
  message_id uuid not null references messages(id)
  user_id    uuid not null references users(id)
  primary key (message_id, user_id)
  index on (user_id, message_id)
```

Written at message insert, extracted from the markdown source. A mention of `@channel` writes one row per active human member. This table drives both unread mention counts and notification routing, so it must be written in the same transaction as the message.

### Read state and notification preferences

```sql
read_markers
  user_id       uuid   not null references users(id)
  container_id  uuid   not null references containers(id)
  last_read_seq bigint not null
  updated_at    timestamptz not null
  primary key (user_id, container_id)

notification_prefs
  user_id      uuid not null references users(id)
  container_id uuid not null references containers(id)
  level        enum('all','mentions','none') not null
  primary key (user_id, container_id)

thread_subscriptions
  user_id        uuid not null references users(id)
  thread_root_id uuid not null references messages(id)
  state          enum('subscribed','muted') not null
  primary key (user_id, thread_root_id)
```

**Read state is per user, never per device.** A message read on a phone must stop being unread on a laptop. Per-device read state produces a badge that will not clear, which is the most irritating defect this class of application has.

Unread counts are derived, not stored:

```
unread(user, container)  = container.last_seq - coalesce(read_marker.last_read_seq, 0)
mentions(user, container) = count of mentions m
                            join messages msg on msg.id = m.message_id
                            where m.user_id = user
                              and msg.container_id = container
                              and msg.seq > coalesce(read_marker.last_read_seq, 0)
                              and msg.deleted_at is null
```

An absent `notification_prefs` row means the default: `mentions` for a channel. Conversations ignore the table entirely and always behave as `all`.

An absent `thread_subscriptions` row means not subscribed. Posting into a thread inserts `subscribed` if no row exists, so participation implicitly subscribes. A user may set `muted` to stop a thread they participated in.

### Attachments

```sql
attachments
  id             uuid primary key
  message_id     uuid null references messages(id)   -- null while staged
  uploader_id    uuid not null references users(id)
  kind           enum('image','video','audio','file') not null
  original_name  text not null
  mime           text not null
  size_bytes     bigint not null
  width          int null
  height         int null
  duration_ms    int null
  storage_path   text not null
  thumb_path     text null
  state          enum('staged','processing','ready','failed') not null
  error          text null
  created_at     timestamptz not null
  index on (state, created_at) where message_id is null
```

An upload is staged first and attached to a message on send. A staged attachment older than `retention.staged_upload_hours` with `message_id is null` is swept and its files deleted.

### Agents

```sql
agents
  user_id           uuid primary key references users(id)
  state             enum('reserved','serving') not null
  claim_token_hash  bytea not null
  allow_history     boolean not null default false
  argv              text[] null
  registered_at     timestamptz null
  last_seen_at      timestamptz null

agent_job_attachments
  job_id        uuid not null references agent_jobs(id)
  attachment_id uuid not null references attachments(id)
  primary key (job_id, attachment_id)

agent_jobs
  id             uuid primary key
  agent_id       uuid not null references users(id)
  container_id   uuid not null references containers(id)
  trigger_msg_id uuid not null references messages(id)
  state          enum('queued','dispatched','done','failed','timeout') not null
  prompt         text not null
  result         text null
  error          text null
  created_at     timestamptz not null
  dispatched_at  timestamptz null
  finished_at    timestamptz null
  index on (agent_id, state) where state in ('queued','dispatched')
```

### Calls

```sql
calls
  id              uuid primary key
  container_id    uuid not null references containers(id)
  room_name       text not null unique       -- "call_" || id
  started_by      uuid not null references users(id)
  started_at      timestamptz not null
  ended_at        timestamptz null
  recording_state enum('off','recording','processing','ready','failed') not null default 'off'
  unique (container_id) where ended_at is null    -- one live call per container

call_participants
  call_id   uuid not null references calls(id)
  user_id   uuid not null references users(id)
  joined_at timestamptz not null
  left_at   timestamptz null

call_recordings
  id           uuid primary key
  call_id      uuid not null references calls(id)
  user_id      uuid null references users(id)   -- null if the track is unattributed
  storage_path text not null
  duration_ms  int null
  created_at   timestamptz not null
```

The partial unique index on `(container_id) where ended_at is null` is what makes "one call per container" a database invariant rather than application logic. Two people pressing call simultaneously means one insert wins and the loser joins the winner's call.

### Push subscriptions

```sql
push_subscriptions
  id              uuid primary key
  user_id         uuid not null references users(id)
  endpoint        text not null unique
  p256dh          text not null
  auth            text not null
  user_agent      text
  enabled         boolean not null default true
  created_at      timestamptz not null
  last_success_at timestamptz null
  last_failure_at timestamptz null
  index on (user_id) where enabled
```

One row per device. `enabled` is the per-device delivery switch that lets a user silence their laptop while keeping their phone loud. `endpoint` is unique because a browser issues one subscription per origin per profile, so re-subscribing upserts rather than duplicating.

## 4. Realtime protocol

One WebSocket per client at `wss://chat.example.com/ws`, authenticated by the session cookie sent on the upgrade request. A user may hold several concurrent sockets, one per open tab or device.

### Envelope

Every frame is JSON with a type tag and a payload.

```json
{"t": "message", "d": { ... }}
```

### Connection handshake

The client opens the socket and immediately sends its per-container cursors. This is the entire resume mechanism.

```json
{"t": "hello", "d": {
    "cursors": {"<container_id>": 4471, "<container_id>": 219}
}}
```

The server responds with the current state and resolves every gap:

```json
{"t": "ready", "d": {
    "user": {...},
    "containers": [
        {"id": "...", "kind": "channel", "slug": "engineering", "name": "Engineering",
         "last_seq": 4480, "unread": 9, "mentions": 1, "level": "mentions"},
        ...
    ],
    "gaps": [{"container_id": "...", "from": 4472, "to": 4480}]
}}
```

Gap resolution rule:

```
for each container the client holds a cursor for:
    gap = container.last_seq - client_cursor
    if gap == 0:            nothing to do
    if gap <= 100:          send the messages inline as individual "message" frames
    if gap  > 100:          list it in "gaps"; the client backfills over HTTP
```

The client does not need to have been connected before. A cold client sends `hello` with no cursors and receives container metadata only, then loads each container's timeline on demand.

### Client to server

| Type | Payload | Effect |
|---|---|---|
| `hello` | `cursors` | Handshake and gap resolution |
| `send` | `container_id`, `client_id`, `body`, `reply_to_id?`, `thread_root_id?`, `attachment_ids?` | Post a message |
| `edit` | `message_id`, `body` | Rewrite own message |
| `delete` | `message_id` | Soft-delete own message, or any message if admin |
| `read` | `container_id`, `seq` | Advance the read marker |
| `typing` | `container_id` | Broadcast a typing indicator |
| `thread_sub` | `thread_root_id`, `state` | Subscribe or mute a thread |
| `ping` | none | Keepalive |

### Server to client

| Type | Payload | When |
|---|---|---|
| `ready` | state and gaps | After `hello` |
| `message` | full message object | A message lands in a container the user can see |
| `message_edited` | `id`, `body`, `edited_at` | An edit |
| `message_deleted` | `id`, `container_id`, `seq` | A delete |
| `read` | `container_id`, `seq` | The same user advanced a read marker on another device |
| `typing` | `container_id`, `user_id` | Someone is typing |
| `presence` | `user_id`, `online` | A user's socket count crossed zero |
| `call_started` | call object | A call opened in a container |
| `call_participant` | `call_id`, `user_id`, `joined` | Someone joined or left a call |
| `call_ended` | `call_id` | The call closed |
| `agent_working` | `container_id`, `agent_id`, `job_id` | An agent job was dispatched |
| `agent_done` | `job_id` | An agent job finished, succeeded or failed |
| `pong` | none | Keepalive response |

The `read` frame going back to the same user is what keeps multi-device read state coherent. When a phone advances a marker, every other socket belonging to that user receives it and clears its badge.

### Sending a message

```
on "send" from user U:
    reject if U.kind == 'agent'
    reject if U is not in members(container)
    if a message exists with (author_id=U, client_id=given):
        return that message           # idempotent retry
    begin
        seq = allocate(container)
        insert message
        insert mentions rows for every @handle and for @channel
        if thread_root_id is set:
            increment root.thread_reply_count
            set root.thread_last_reply_at
            upsert thread_subscriptions(U, root) = 'subscribed'
        bind staged attachments to the message
    commit
    broadcast "message" to every live socket of every member
    enqueue notification routing (section 8)
    enqueue agent dispatch for every mentioned agent (section 7)
```

Broadcast and notification routing happen after commit, never inside the transaction. A failed push must not roll back a stored message.

### Keepalive and reconnect

The server sends a WebSocket ping every 30 seconds and closes a socket that misses two. The client reconnects with exponential backoff starting at 1 second, capped at 30 seconds, with jitter. Every reconnect replays the `hello` handshake, so a gap of any size self-heals.

Mobile browsers suspend sockets aggressively when backgrounded. Treat a dropped socket as normal operation rather than an error, and never surface it to the user unless reconnection has failed for more than about 15 seconds.

## 5. HTTP API

All endpoints are under `/api`, take and return JSON, and authenticate with the session cookie. The WebSocket carries live traffic; these endpoints carry everything a cold client or a backfill needs.

### Authentication

```
POST   /api/auth/login              {handle|email, password} -> sets cookie
POST   /api/auth/logout             -> clears cookie, deletes session
GET    /api/auth/me                 -> current user
POST   /api/auth/accept-invite      {token, handle, display_name, password}
POST   /api/auth/password           {current_password, new_password}
```

### Containers and messages

```
GET    /api/containers
GET    /api/containers/:id/messages?before=<seq>&limit=50
GET    /api/containers/:id/messages?after=<seq>&limit=50
GET    /api/containers/:id/messages?around=<seq>&limit=50
GET    /api/threads/:root_id/messages?after=<seq>&limit=50
POST   /api/containers/:id/messages           -- fallback when the socket is down
PATCH  /api/messages/:id                      {body}
DELETE /api/messages/:id
PUT    /api/containers/:id/read               {seq}
PUT    /api/containers/:id/notification-pref  {level}
PUT    /api/threads/:root_id/subscription     {state}
POST   /api/conversations                     {user_ids: []} -> container
GET    /api/users
GET    /api/search?q=<query>&container_id=<id?>&limit=50
```

The three cursor forms on the message endpoint each serve a distinct need:

- `before` pages backwards for scrollback.
- `after` fills a reconnect gap forwards.
- `around` loads a window centred on one message, which is what a notification tap or a search result needs.

Search uses the `search_tsv` generated column with `websearch_to_tsquery`, ranked by `ts_rank_cd`, filtered to containers the requesting user can see.

### Media

```
POST   /api/uploads                 multipart -> staged attachment
GET    /api/attachments/:id         -> file, with a long cache header
GET    /api/attachments/:id/thumb   -> thumbnail
```

### Push

```
GET    /api/push/vapid-key          -> the public key, for subscribe()
POST   /api/push/subscribe          {endpoint, keys:{p256dh, auth}} -> upsert
DELETE /api/push/subscribe          {endpoint}
PUT    /api/push/subscribe/enabled  {endpoint, enabled}
```

### Calls

```
POST   /api/containers/:id/call     -> creates or returns the live call
GET    /api/calls/:id/token         -> a signed LiveKit join token
DELETE /api/calls/:id/me            -> leave
POST   /api/calls/:id/recording     {on|off}
GET    /api/calls/:id/recordings
POST   /api/livekit/webhook         -- LiveKit server callbacks, signature-verified
```

### Agent daemon

These are the only endpoints authenticated by an agent claim token rather than a session cookie. The token travels as `Authorization: Bearer <token>`.

```
POST   /api/agent/register          {handle, claim_token, argv, allow_history}
POST   /api/agent/deregister        {handle, claim_token, release?}
GET    /api/agent/jobs              -- long-poll or SSE stream of dispatched jobs
POST   /api/agent/jobs/:id/result   {result} or {error}
GET    /api/agent/jobs/:id/attachments/:attachment_id  -- scoped file fetch
POST   /api/agent/hello             -- heartbeat, updates last_seen_at
```

### Admin

Every endpoint requires `users.is_admin`.

```
GET    /api/admin/users
POST   /api/admin/users             {handle, display_name, email}
DELETE /api/admin/users/:id         -- deactivate, never hard delete
POST   /api/admin/users/:id/password  -- admin reset
POST   /api/admin/invites           {note, expires_in} -> one-time URL
GET    /api/admin/invites
POST   /api/admin/channels          {slug, name, topic}
PATCH  /api/admin/channels/:id
DELETE /api/admin/channels/:id      -- archive
POST   /api/admin/agents            {handle, display_name} -> reserve, returns claim token
DELETE /api/admin/agents/:handle
GET    /api/admin/stats
```

## 6. Authentication and accounts

There is no open signup and no email delivery. The deployment runs without a mail server.

### Bootstrap

On first start with an empty `users` table, the application prints a one-time invite URL to stdout and to the log. Opening it creates the first account with `is_admin = true`. The URL is not persisted anywhere else.

### Invites

An admin creates an invite, which yields a URL of the form `https://chat.example.com/invite/<token>`. The raw token exists only in that URL; the database holds its SHA-256. Accepting an invite consumes it atomically, so a token cannot be redeemed twice.

### Passwords

Hash with argon2id from `golang.org/x/crypto/argon2`. Verify with a constant-time comparison. There is no self-service reset, because there is no mail path. An admin resets a password from the admin interface, which is the correct trade for a twenty-person team.

### Sessions

The cookie is `Secure`, `HttpOnly`, `SameSite=Lax`, and `Path=/`. Sessions last 90 days and their `expires_at` extends on use. Logging out deletes the row so the cookie is dead immediately rather than merely unused.

`SameSite=Lax` rather than `Strict` because a notification tap navigates from outside the origin and must arrive authenticated.

## 7. Agents

An agent is a user row with `kind = 'agent'`. It appears in the member list, is mentioned with `@handle`, and its answers are ordinary messages with `author_id` pointing at it. Everything that renders a human message renders an agent message unchanged.

### How an agent differs from a human

| Property | Human | Agent |
|---|---|---|
| Authenticates with | Session cookie from a password | Claim token as a bearer header |
| Has `email`, `password_hash` | Yes | No |
| Counted in `members()` | Yes | No |
| Can open a WebSocket | Yes | No |
| Can be a conversation participant | Yes | No |
| Receives notifications | Yes | No |
| Posts messages | Directly | Only as a job result |
| Created by | Invite acceptance | Admin reservation |
| Sees message history | All containers | Only when `allow_history` is set |

An agent has no socket and no notification path because it is not a client. It is a daemon that receives dispatched jobs and returns results.

### Where the agent runs

The agent daemon runs on its owner's own machine, not on the VPS. The server never executes a model, holds an API key, or runs a subprocess. It dispatches a prompt and receives an answer.

This is deliberate. Putting agent execution on the server would put every owner's prompts, credentials, and local skill files on one box. The daemon holds the model credentials; the server holds only the claim token.

### Lifecycle

Three states, mirroring a proven design. The transitions are the whole state machine.

```
                          absent
                            |  admin reserves the handle
                            v
                        reserved  <-------------------+
                            |  daemon registers        |
                            v                          |
                        serving  ---- deregister ------+

    admin delete, from either state, returns to absent
```

**Reserve.** An admin creates the agent from the admin interface. The server creates the `users` row with `kind = 'agent'`, creates the `agents` row in state `reserved`, and mints a 32-byte claim token, returning it once and storing only its hash. The handle is now held.

**Register.** The owner's daemon calls `POST /api/agent/register` with the handle, the claim token, the argv to execute, and whether history retrieval is allowed. The server verifies the token hash, stores `argv` and `allow_history`, and moves to `serving`. Dispatch is now live.

**Deregister.** Returns to `reserved`. The handle stays held, the user row survives, and message history is untouched. This is how an agent's behaviour is changed: deregister, change the argv, register again.

**Delete.** An admin deletes the agent from the admin interface, from either state. There is no daemon-side release.

Deletion branches on whether the agent ever spoke:

```
delete(agent):
    if the agent has authored no messages:
        delete the agents row and the users row     # handle is free again
    else:
        delete the agents row
        set users.deactivated_at                    # handle stays held
```

An agent that posted cannot have its handle freed, because reusing it would reattribute old messages to a different agent. An agent that was created, misconfigured, and never used deletes cleanly, which is the common case.

A claim token is required on register, deregister, and every heartbeat. Without it the last daemon to announce a handle would silently take it over.

### Dispatch

```
after a message M commits:
    for each agent A mentioned in M:
        if A.state != 'serving':
            post a system message "agent @A is not serving" and stop
        if an unfinished job exists for A in this container:
            post "agent @A is already working" and stop
        prompt = compose_prompt(A, M)
        insert agent_jobs (state='queued', prompt)
        broadcast "agent_working"
```

The daemon holds an open request against `GET /api/agent/jobs` and receives queued jobs. On receipt the server moves the job to `dispatched` and stamps `dispatched_at`.

### Prompt composition

Composition happens on the server, because the server is the only party that can read history and enforce `allow_history`.

```
compose_prompt(agent, message):
    parts = []
    parts += "You are @{agent.handle} in the {container.name} channel."
    parts += "The message addressed to you:"
    parts += message.body
    if agent.allow_history:
        parts += "Recent conversation, oldest first:"
        parts += last 50 messages in container before message.seq,
                 rendered as "@handle: body"
    if message.thread_root_id is not null:
        parts += "This is a reply in a thread. The thread so far:"
        parts += every message in that thread, oldest first
    return join(parts, "\n\n")
```

`allow_history` defaults to false. An agent that has not been granted history sees only the message that mentioned it, which is the safe default for an input that is attacker-controlled by construction.

### Results

The daemon returns the answer to `POST /api/agent/jobs/:id/result`. The server:

1. Verifies the claim token and that the job belongs to this agent and is in `dispatched`.
2. Rejects an empty result as a failure. An agent that produced nothing has failed, and posting an empty message hides that.
3. Stores the result verbatim as the message body. There is no server-side rendering.
4. Inserts a message with `author_id = agent`, `thread_root_id` inherited from the triggering message if it had one, and `reply_to_id` pointing at the triggering message otherwise.
5. Moves the job to `done` and broadcasts `agent_done`.

A job that exceeds `agents.job_timeout` moves to `timeout` and posts a failure message naming the agent and the elapsed time. A failure is always visible in the channel; a silent agent is the worst outcome because nobody can tell whether it is thinking or dead.

### Agent output format

Agents emit markdown, identical in every respect to what a human types. There is no separate agent format and no server-side conversion. Whatever the agent returns is stored as the message body and rendered by the frontend exactly as a human message is.

The prompt tells the agent that its answer is rendered as markdown, that GFM tables are supported, and that a ```mermaid fence renders as a diagram. Nothing else needs saying, because the agent and the human write into the same renderer.

### File retrieval

An agent may fetch any attachment it was shown. There is no type restriction. Images, audio, video, PDFs, archives, and arbitrary files are all retrievable, and the agent decides what is worth reading.

Prompt composition lists every attachment on every message it includes:

```
@tanq: here is the quarterly export, can you summarise the revenue table
[attachment id=a3f91c02 name=q3-export.pdf size=2.4MB]
[attachment id=7bd4e910 name=board-call.ogg size=18.1MB]
```

The prompt closes with an instruction naming a helper the daemon wrote into the job directory:

```
To read any attachment listed above, run: ./fetch-attachment <id>
It writes the file into ./files/ and prints the path. Fetch only what you need.
```

The daemon does not pre-fetch. A thread carrying a 200 MB video must not cost 200 MB of transfer because someone mentioned an agent in it. The helper is a small script the daemon writes per job; it calls the server with the job's claim token and streams the file to disk.

Retrieval is scoped to what the agent was shown:

```
GET /api/agent/jobs/:job_id/attachments/:attachment_id
    authenticate the claim token -> agent
    reject unless the job belongs to this agent and is 'dispatched'
    reject unless (job_id, attachment_id) exists in agent_job_attachments
    stream the file
```

`agent_job_attachments` rows are written at compose time, one per attachment named in the prompt. Without that table an agent could enumerate attachment ids across the whole deployment. With it, the agent's reach over files is exactly the boundary `allow_history` draws over message text.

### Interactive sessions

The job model above is single-shot: one mention produces one answer. A future interactive mode reuses the same tables by treating a thread as a session, keeping the daemon's model process alive between jobs whose `thread_root_id` matches. Build the single-shot path first. The schema already supports the extension because `agent_jobs` carries `container_id` and the triggering message.

## 8. Notifications

### Routing

This decides who gets pushed. It is the difference between an application people keep installed and one they mute in the first week.

```
route(message M in container C, author A):
    for each user U in members(C), U != A, U.kind == 'human':
        if not should_notify(U, M, C):
            continue
        deliver(U, M)

should_notify(U, M, C):
    if C.kind == 'conversation':
        return true                        # DMs and group DMs always notify

    level = notification_prefs(U, C) or 'mentions'
    if level == 'none':
        return false

    if M.thread_root_id is not null:
        sub = thread_subscriptions(U, M.thread_root_id)
        if sub == 'muted':
            return false
        if mentioned(U, M):
            return true
        return sub == 'subscribed' or level == 'all'

    if level == 'all':
        return true
    return mentioned(U, M)
```

The three channel levels are `all`, `mentions`, and `none`, defaulting to `mentions`. Conversations have no setting and always notify. A mention always breaks through a channel set to `mentions`, and always breaks through thread inheritance, but never breaks through `none` or a muted thread. `none` and `muted` are the user saying stop, and a system that overrides that is one the user stops trusting.

### Delivery

```
deliver(U, M):
    payload = build_payload(M)
    for each S in push_subscriptions(U) where S.enabled:
        if S.device has a live WebSocket seen within 30 seconds:
            continue                       # that tab renders it in-page instead
        send_push(S, payload)
```

**Suppress push to a device that is currently connected, and push every other device.** A laptop with the tab open shows an in-page notification through the Notifications API with no push involved. A phone sitting idle gets a push. The user needs no setting for the case they care about most, which is being at their desk and still wanting their phone to buzz.

Matching a socket to a subscription requires the client to send its push endpoint on the `hello` frame, so the server can associate the two. Without it, fall back to suppressing when the user has any live socket, which is worse but correct.

### Payload

Under 3993 bytes of plaintext after encryption overhead. Truncate the body to 120 characters.

Because nothing is rendered server-side, the notification body needs a plaintext reduction of the markdown source: strip fences, links down to their text, emphasis markers, and heading hashes. This is a small regex pass and not a parser. It only has to be readable in a notification shade, and a stray asterisk surviving into a banner is not a defect worth a dependency.

```json
{
    "web_push": 8030,
    "notification": {
        "title": "#engineering",
        "body": "tanq: the deploy finished",
        "navigate": "https://chat.example.com/c/engineering?m=4481",
        "app_badge": "3"
    },
    "x": {"container_id": "...", "message_id": "...", "seq": 4481}
}
```

The `web_push: 8030` member opts into Declarative Web Push, which newer Safari renders natively with no service worker involvement. Older browsers ignore it and the service worker handles the same payload. The two paths are compatible, so send one payload shape to everything.

Title is the channel name for a channel and the sender's display name for a conversation. `app_badge` carries the user's total unread mention count across all containers, computed at send time.

### Sending

Depend on the Go library `github.com/SherClockHolmes/webpush-go`. It is roughly 457 lines against four frozen RFCs, is the library the Go ecosystem actually uses, and its small size is the reason to depend on it rather than the reason to reimplement it. Vendor the module and pin a commit rather than a tag.

Four wrappers are required around it, each a few lines:

1. **Strip a `mailto:` prefix from the subscriber before passing it.** The library prepends its own, producing `mailto:mailto:...`, which Apple rejects with 403 `BadJwtToken`.
2. **Check the HTTP status yourself.** `SendNotification` returns a non-2xx response with a nil error, so status handling is entirely the caller's job.
3. **Set `RecordSize`.** The default pads every message to the full 4096-byte record, which sits exactly on Apple's payload limit for a two-word notification.
4. **Set `Urgency: high`.** Required in practice for timely iOS delivery.

Apple needs nothing beyond standard VAPID and no Apple Developer account. Its one strictness beyond the RFC is that the JWT `sub` claim is effectively mandatory and must be a `mailto:` or `https:` URI. Do not refresh the VAPID JWT more than once per hour.

### Subscription lifecycle

Subscriptions rot. Three mechanisms handle it, in ascending order of how much to rely on them.

**On send failure.** Delete the subscription row on `404` or `410`. The RFC specifies 404 for an expired subscription and every vendor sends 410, so handle both. Treat `429` as backoff, not as death, and leave the row alone.

**On `pushsubscriptionchange`.** The service worker handles the event by re-subscribing and posting the new subscription. Browser support for this event is uneven, so it is a bonus rather than a mechanism.

**On every application load.** Read the current subscription from the service worker registration and upsert it to the server. This is the one that actually keeps things alive, because it self-heals whatever the other two missed without depending on either working.

### Service worker

```js
self.addEventListener('push', (event) => {
    const data = event.data.json()
    const n = data.notification
    event.waitUntil(
        self.registration.showNotification(n.title, {
            body: n.body,
            data: {navigate: n.navigate, ...data.x},
            tag: data.x.container_id,        // collapse per container
            renotify: true
        })
    )
})

self.addEventListener('notificationclick', (event) => {
    event.notification.close()
    const url = event.notification.data.navigate
    event.waitUntil(
        clients.matchAll({type: 'window', includeUncontrolled: true})
            .then(list => {
                for (const c of list) {
                    if (c.url.startsWith(self.registration.scope)) {
                        c.navigate(url)
                        return c.focus()
                    }
                }
                return clients.openWindow(url)
            })
    )
})
```

**Every push must call `showNotification`.** Failing to do so revokes the subscription after repeated offences on iOS. There is no silent push, so never use push as a background sync trigger.

`tag` set to the container id collapses successive notifications from the same channel into one, which is what stops a busy channel from filling the notification shade.

## 9. Calls

One call type. There is no separate voice call and video call, because turning a camera off already stops the capture track and publishes no video RTP at all. An audio-only call costs no video bandwidth without being modelled as a different thing, and splitting the two would only remove screen share from the audio variant.

### Lifecycle

```
POST /api/containers/:id/call:
    if a call exists for this container with ended_at is null:
        return it
    insert calls (container_id, room_name = "call_" || new uuid, started_by, started_at)
    broadcast "call_started" to every member
    return the call
```

The partial unique index on `(container_id) where ended_at is null` makes concurrent starts safe. One insert wins, the loser catches the constraint violation and returns the existing call.

Joining is minting a token, not a state change. A client calls `GET /api/calls/:id/token`, connects to LiveKit directly with it, and the server learns who actually joined from LiveKit's webhooks rather than from the client's claim.

### Token minting

The server signs a JWT with the LiveKit API secret. This is the only integration point and it is small.

```
token(call, user):
    claims = {
        iss: config.livekit.api_key,
        sub: user.id,
        nbf: now,
        exp: now + 6h,
        name: user.display_name,
        video: {
            room:          call.room_name,
            roomJoin:      true,
            canPublish:    true,
            canSubscribe:  true,
            canPublishData: true
        }
    }
    return HS256(claims, config.livekit.api_secret)
```

`sub` is the user id so LiveKit participant identity maps back to a row without a lookup table. Never mint a token for a user who is not in `members(container)`.

### Webhooks

LiveKit posts to `POST /api/livekit/webhook`, signed with the API key. Verify the signature and reject anything unsigned.

| Event | Server action |
|---|---|
| `participant_joined` | Insert `call_participants`, broadcast `call_participant` |
| `participant_left` | Stamp `left_at`, broadcast `call_participant` |
| `room_finished` | Stamp `calls.ended_at`, stop egress, broadcast `call_ended` |

### Reaper

A call whose last participant's browser crashed never receives a clean leave. A background job every 60 seconds closes any call with no participant rows lacking `left_at`, and where LiveKit's room API reports no room. Without this, `call_started` state accumulates and the container unique index blocks new calls.

### Recording

Recording is per-track, not room-composite. Track egress writes one audio file per participant and does not launch a headless browser, so it costs a fraction of composite recording. Per-speaker files are also strictly better input for transcription, because speaker attribution comes free and diarization is unnecessary.

```
POST /api/calls/:id/recording {on}:
    for each published audio track in the room:
        start a TrackEgress writing to /out/<call_id>/<user_id>.ogg
    set calls.recording_state = 'recording'
```

A participant joining mid-recording gets an egress started for their track on the `participant_joined` webhook. On `room_finished`, stop every egress, move `recording_state` to `processing`, and insert a `call_recordings` row per file once egress reports completion.

Never run room-composite egress on this VPS. It is documented at 2 to 6 CPUs per concurrent job and would be the only component in the deployment that genuinely competes for CPU.

### Future transcription

Out of scope for the first build, and the schema already accommodates it. Per-speaker files plus their `created_at` offsets give timestamped segments per speaker, which assemble into a single ordered transcript deterministically without a diarization model. Add a `call_transcripts` table when the feature is built.

### Client behaviour

Publish settings come from `media_quality` in the server config, delivered to the client in the `ready` frame so a change needs no rebuild.

- `adaptiveStream: true` and `dynacast: true`. Both are required. Adaptive stream picks the simulcast layer matching the rendered tile height; dynacast stops publishing layers nobody watches. Together they are the difference between 3.1 Mbps and 10.5 Mbps for a three-person call.
- Camera off by default on join. The user opts into video.
- `echoCancellation`, `noiseSuppression`, and `autoGainControl` all true in the `getUserMedia` audio constraints. The browser implements these with the same libwebrtc code every major conferencing product ships, so no additional library or model is needed.
- Screen share is available on desktop only. Hide the control when `navigator.mediaDevices.getDisplayMedia` is undefined, which is every mobile browser.

### Screen wake lock

While a call is active, hold a screen wake lock so the device does not dim or lock.

```js
let lock = null
async function acquire() {
    try { lock = await navigator.wakeLock.request('screen') } catch {}
}
document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && callActive) acquire()
})
```

The lock is released automatically whenever the page is hidden, so it must be re-acquired on `visibilitychange` or a tab switch loses it permanently.

For idle dimming, keep the wake lock and apply a dark overlay in the application after a few minutes without input. This keeps the call alive and puts the dimming behaviour under application control rather than the operating system's.

### Call notification

Starting a call posts a system message into the container and routes it through the ordinary notification path. It is a notification, not a ring. No web technology can produce a ringing incoming call on iOS, and attempting one produces a worse result than a clear notification.

## 10. Media pipeline

### Upload

`POST /api/uploads` accepts multipart, enforces `media.max_upload_bytes`, and writes the file to a staging path. It inserts an `attachments` row in state `staged` with `message_id` null and returns the id. The client sends that id in the message's `attachment_ids`.

Determine `kind` and `mime` by sniffing content, never by trusting the client's declared type or the filename extension.

### Processing

Processing is asynchronous and moves the row through `processing` to `ready` or `failed`. The client renders a placeholder until `ready`.

| Kind | Action |
|---|---|
| Image | Strip EXIF, downscale to `media.image_max_dimension`, convert to WebP, generate a thumbnail |
| Video | Probe for dimensions and duration, extract a poster frame as the thumbnail, leave the file otherwise untouched |
| Audio | Probe for duration; no transcode |
| File | Store as uploaded |

Stripping EXIF is not optional. Phone photographs carry GPS coordinates, and a chat platform that republishes them leaks location for every image anyone posts.

`ffmpeg` and `imagemagick` are invoked as subprocesses from the application container. Bound every invocation with a timeout and a memory limit, because they run against files that arrived from outside.

### Serving

Attachments serve from `GET /api/attachments/:id` behind a session check. Set a long `Cache-Control` with `immutable`, since an attachment id never changes content.

### Sweeping

A staged attachment with `message_id is null` older than `retention.staged_upload_hours` is deleted along with its files. An attachment whose message was hard-deleted by retention is swept the same way.

## 11. Frontend

Vanilla HTML, Tailwind, and plain JavaScript modules. No framework and no bundler. Everything is embedded into the Go binary with `go:embed` and served from memory.

### Layout

```
web/
  index.html
  manifest.json
  sw.js
  css/app.css                built Tailwind output
  js/
    main.js                  bootstrap, routing
    api.js                   fetch wrappers, error handling
    socket.js                WebSocket, backoff, gap fill
    store.js                 in-memory state, subscriptions
    push.js                  subscribe, upsert on load
    render.js                marked, mermaid, highlight; the only renderer
    ui/
      timeline.js
      thread.js
      composer.js
      sidebar.js
      call.js
      admin.js
  vendor/
    fonts/                   self-hosted, no external requests
    marked.min.js
    mermaid.min.js
    highlight.min.js
```

### State

One in-memory store holding containers, the loaded message window per container, read markers, and call state. Views subscribe to store changes. There is no persistence beyond `localStorage` for the composer draft and the last open container.

### Scrollback

Paged, not virtualized. A channel is never fully loaded because the client only holds what has been scrolled through.

```
on scroll within 300px of top:
    if loading or no more history: return
    prevHeight = el.scrollHeight
    fetch /api/containers/:id/messages?before=<oldest loaded seq>&limit=50
    prepend the results
    el.scrollTop += el.scrollHeight - prevHeight     # anchor the viewport
```

Capturing `scrollHeight` before insertion and restoring after is what keeps the viewport still while older content is prepended. Do not rely on `overflow-anchor` alone.

Jumping to a message from a notification or a search result loads with `around=<seq>`, which returns a window centred on the target rather than a page from either end.

### Composer

The composer holds markdown source. Its preview runs the same `render.js` path the timeline runs, so what the author sees before sending is exactly what everyone sees after.

Sending is optimistic: generate a `client_id`, render the message immediately in a pending state, and reconcile when the server's `message` frame arrives carrying the same `client_id`. On reconnect the client re-sends any message still pending, and the unique constraint on `(author_id, client_id)` makes that safe.

### Rendering

**Markdown is the wire format. Nothing is converted server-side.** A message body is stored, transmitted, searched, and returned as the markdown the author typed. The frontend is the only renderer, and it renders human and agent messages through the same path with no branch between them.

The pipeline is marked for markdown, highlight.js for code fences, and mermaid for ```mermaid fences. The house conventions for all three, including table styling, callout blockquotes, code copy buttons, and the diagram theme, are defined in the `web-markdown-rendering` and `web-mermaid-diagrams` skills. Read both in full before writing `render.js` rather than inventing conventions here.

Three requirements this spec does impose:

- **Escape raw HTML.** Configure marked so HTML in the source is escaped rather than passed through, and sanitize the output before insertion. Rendering moved from the server to the client, and with it the XSS boundary. A message body is attacker-controlled text from an agent or a person, and the renderer is now the only thing standing between it and the DOM.
- **Set mermaid `securityLevel: 'strict'` and never `startOnLoad`.** Diagrams render on demand from message bodies, which are untrusted input, so mermaid must not be allowed to inject HTML or scripts of its own.
- **Render once per message and cache the resulting node.** Re-rendering markdown and re-running mermaid on every scroll frame is the one way this design becomes slow.

Loose lists, where a model writes blank lines between numbered items and each item renders inside a paragraph, are fixed in CSS rather than by transforming the source. Owning the stylesheet makes this a one-line rule instead of a parser change.

### Threads and replies

Two visibly different actions on a message:

- **Reply** quotes the message inline and posts into the main timeline with `reply_to_id`.
- **Thread** opens a side pane and posts with `thread_root_id`.

A message with `thread_reply_count > 0` renders a thread summary in the timeline showing the count and the last reply time.

### Progressive Web App

`manifest.json` needs `name`, `short_name`, `start_url`, `display: "standalone"`, `theme_color`, `background_color`, and icons at 192 and 512 pixels square.

Installation on iOS is Share, then Add to Home Screen. There is no install prompt on iOS and no way to trigger one, so the application shows a one-time instruction card on iOS Safari when it detects it is not running standalone.

The service worker handles push and notification clicks. It does not cache application assets. Offline support is out of scope, and a stale cached shell against a changed API is a worse failure than a network error.

### Notification permission

Request it from a user gesture, never on load. A push subscription may only be requested in response to a click or keystroke, and an unprompted request is denied permanently by the browser.

Flow: a dismissible banner offers to enable notifications, the click requests permission, then subscribes with the VAPID public key from `GET /api/push/vapid-key`, then posts the subscription.

On every application load, unconditionally read `registration.pushManager.getSubscription()` and upsert whatever it returns. This is the self-healing step that keeps subscriptions alive across browser rotation.

### Responsive behaviour

One layout, breakpoint-driven. Below the mobile breakpoint the sidebar becomes a drawer, the thread pane becomes a full-screen view, and the screen-share control is hidden.

## 12. Admin interface

Served from the same binary at `/admin`, gated on `users.is_admin`. It is a functional surface, not a product.

- **Users.** List, create, deactivate, reset a password, toggle admin.
- **Invites.** Create with a note and an expiry, list outstanding, revoke. Creating one displays the URL once.
- **Channels.** Create, rename, set a topic, archive. Archiving hides a channel and blocks new messages while keeping history.
- **Agents.** Reserve a handle, which displays the claim token once. Show state, `last_seen_at`, and registered argv. Deregister, or delete the agent outright.
- **Stats.** Message count, storage used by media and recordings, active sessions, database size.
- **Retention.** Show the configured policy and when the sweep last ran.

## 13. Backend layout

```
cmd/chat/main.go            entry point, flag parsing, Execute
internal/config/            config file and environment loading
internal/store/             every database query, one file per entity
  migrations/               embedded SQL
internal/http/              router, handlers, middleware
  handlers/                 one file per API group
internal/socket/            WebSocket hub, per-user socket registry, broadcast
internal/push/              webpush-go wrapper, routing, subscription lifecycle
internal/calls/             LiveKit token minting, webhooks, egress control
internal/media/             upload, transcode, sweep
internal/agents/            reserve, register, dispatch, results
internal/retention/         scheduled sweeps
web/                        embedded frontend
```

Errors wrap with `fmt.Errorf` at the boundary they cross, and the HTTP layer is the only place an error becomes a status code. Log with structured output; the two levels are normal operation and `--debug`.

The socket hub holds a map from user id to a set of live sockets. Broadcasting to a container resolves `members()` then fans out to every socket of every member. At this scale a mutex-guarded map is correct and a pub/sub bus is not needed.

## 14. Operations

### Backups

The complete state is the Postgres database plus `data/media`. Both must be captured together.

```
pg_dump -Fc -U chat chat | gzip > backup/db-$(date +%F).sql.gz
tar czf backup/media-$(date +%F).tar.gz data/media
```

Run daily from cron on the host and copy off the box. Recordings dominate the media size, so apply `retention.recording_days` before sizing the backup target.

### Retention

A daily job:

1. Sweep staged attachments older than `retention.staged_upload_hours` with no message.
2. Delete recordings older than `retention.recording_days` and their `call_recordings` rows.
3. If `retention.message_days` is non-zero, hard-delete messages older than that and sweep the attachments orphaned by it.

`retention.message_days: 0` disables message deletion and is the default. Deleting messages leaves `seq` gaps, which clients tolerate because they page by cursor rather than counting.

### Diagnosing a silent agent

In order:

1. Check `agent_jobs` for a row. No row means the mention was not extracted, so check the `mentions` table for the triggering message.
2. A row stuck in `queued` means no daemon is polling. Check `agents.last_seen_at`.
3. A row stuck in `dispatched` means the daemon took the job and did not answer. Check the daemon's own logs on the owner's machine.
4. A row in `failed` or `timeout` carries the reason in `error`, and the same text was posted to the channel.

### Diagnosing a missing notification

In order:

1. Confirm the user has a `push_subscriptions` row with `enabled = true`. No row means the browser never subscribed, most often because permission was never granted from a gesture.
2. Confirm the device did not have a live socket, since a connected device is suppressed by design.
3. Run the routing predicate by hand against the user, message, and container. A channel at its `mentions` default with no mention is working correctly.
4. Check the last send's status. A 404 or 410 means the row should already have been deleted.

## 15. Build and release

A `Makefile` with the usual targets: `build` producing a static binary with the version injected through `-ldflags`, `assets` downloading and vendoring pinned frontend assets into `web/vendor/`, which is fonts plus `marked`, `mermaid`, and `highlight.js` at pinned versions, `docker` building the image, and `run` for local development.

Local development runs against `http://localhost:8080` with no certificate. `localhost` is a secure context by definition, so service workers and push subscriptions work there without any TLS setup. They do not work against a self-signed certificate on an IP address, which is why `insecure: true` is a development affordance and not a deployment mode.

## 16. Implementation order

Each step leaves the application in a state that runs.

1. **Skeleton.** Config loading, Postgres connection, embedded migrations, health endpoint, `go:embed` frontend serving a static page.
2. **Identity.** Users, sessions, invites, the bootstrap invite on an empty database, login and logout, the `/api/auth/me` endpoint.
3. **Containers and messages.** Channels, the `seq` allocator, message insert with markdown rendering, the three cursor forms of the message endpoint. Prove ordering and idempotency before building anything on top.
4. **WebSocket.** The hub, `hello` and `ready`, gap resolution, live message broadcast, optimistic send reconciliation on the client.
5. **Frontend core.** Sidebar, timeline, composer, scrollback with viewport anchoring, read markers.
6. **Threads and replies.** Both mechanisms, the thread pane, thread summaries in the timeline.
7. **Conversations.** DMs and group DMs as the second container kind.
8. **Notifications.** VAPID keys, subscription endpoints, the service worker, the routing predicate, per-device suppression, subscription lifecycle. Test on a real iOS home-screen install before moving on, because it is the only surface that cannot be verified on a desktop.
9. **Media.** Upload, staging, transcode, thumbnails, timeline rendering, the sweep.
10. **Agents.** Reserve, register, dispatch, results, timeouts, the admin surface for all of it.
11. **Calls.** LiveKit deployment, token minting, webhooks, the reaper, the call UI, wake lock, screen share on desktop.
12. **Recording.** Track egress, the recordings table, retention.
13. **Admin and operations.** The remaining admin surface, search, backups, the retention job.

Steps 3, 4, and 8 carry the risk. Message ordering and reconnect correctness decide whether the application feels trustworthy, and iOS push is the only part that cannot be validated without the real device.

## 17. Decisions not to revisit

An implementing agent will be tempted to change these. Each was decided deliberately.

- **No membership table for channels.** Every active human is in every channel. This removes access control from the entire codebase.
- **`seq`, never timestamps, for ordering and cursors.** Clocks skew and jump.
- **Read state per user, delivery per device.** These are different concepts and merging them produces badges that will not clear.
- **One call type.** Cameras off already publish nothing.
- **Track egress, never room composite.** Composite costs 2 to 6 CPUs and produces a worse input for transcription.
- **Agents run on their owners' machines.** The server holds no model credentials.
- **Markdown is the wire format and the frontend is the only renderer.** No `body_html`, no server-side conversion, no separate agent format.
- **Paged scrollback, not virtualization.** The client never holds a full channel.
- **No service worker asset caching.** A stale shell against a live API fails worse than a network error.
- **No `--release` on the agent daemon.** Deleting an agent is an admin action, and an agent that has posted keeps its handle so history stays attributable.
- **Agent file retrieval is unrestricted by type and scoped by job.** The agent decides what is worth reading; `agent_job_attachments` decides what it is allowed to reach.
- **Depend on `webpush-go`, do not reimplement it.** The protocol is frozen and the library's small size is why it is safe to depend on.
- **`vp8` only.** VP9 and AV1 are cheaper and absent from Safari.

## 18. Still to decide

These are open. Settle them during implementation rather than guessing now.

- Whether `@channel` is available to everyone or to admins only. The schema supports either.
- Whether an archived channel remains visible to members or only to admins.
- Whether interactive multi-turn agent sessions arrive in the first year, which affects nothing in the schema but does affect how the daemon holds process state.
- Whether TURN needs a separate coturn container or LiveKit's built-in server suffices for the deployment's client population.
- Avatar handling: whether uploads go through the same attachment pipeline or a narrower one.
