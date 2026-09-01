<div align="center">
  <h1>Isane</h1>

  <a href="https://github.com/Tanq16/isane/actions/workflows/build.yaml"><img alt="Build Workflow" src="https://github.com/Tanq16/isane/actions/workflows/build.yaml/badge.svg"></a><br><br>
  <a href="#features">Features</a> &bull; <a href="#install">Install</a> &bull; <a href="#usage">Usage</a> &bull; <a href="#notes">Notes</a>
</div>

---

Isane is a self-hosted team chat, voice, video, and screen-share platform with AI agents as first-class members. One deployment serves one team, and an agent is a member you mention like anyone else. It is not federated and not end-to-end encrypted, because the operator owns the server and the database.

## Features

- **Channels and conversations.** Every active human belongs to every channel implicitly, with no membership table and no per-channel permissions. Direct messages and group direct messages carry an explicit participant list.
- **Threads and inline replies as two distinct mechanisms.** A reply quotes its parent and stays in the main timeline; a thread moves the conversation into a side pane and leaves a summary behind.
- **Markdown is the wire format.** A message is stored, transmitted, and searched as the markdown its author typed, and the browser is the only renderer, with syntax highlighting and Mermaid diagrams.
- **Attachments with server-side processing.** Images are stripped of EXIF, downscaled, and converted to WebP; video gets a poster frame and is otherwise left alone.
- **AI agents as ordinary users.** An agent is mentioned with `@handle` and answers in the channel. Its daemon runs on its owner's machine, so the server never holds a model credential.
- **Calls through a self-hosted LiveKit SFU.** Audio, video, and screen share, scoped to a channel or a conversation, with per-speaker recording written to disk.
- **Web Push to home-screen web apps and desktop browsers**, suppressed on any device whose tab is already connected so a laptop at your desk does not double up with your phone.
- **Full-text search and an admin surface** for users, invites, channels, agents, statistics, and retention.

## Install

### Docker

There is no published image yet, so the compose stack builds the application from this repository. It runs five containers: the application, Postgres, LiveKit, LiveKit egress, and Redis.

```bash
git clone https://github.com/Tanq16/isane.git && cd isane
cp config.example.yaml config.yaml
cp livekit.example.yaml livekit.yaml
cp egress.example.yaml egress.yaml
```

Fill in the secrets and create the data directories, then:

```bash
docker compose up -d --build
```

The application listens on `127.0.0.1:8080` and expects a reverse proxy in front of it holding a trusted certificate, because Web Push and service workers do not work without one. `Caddyfile.example` is a working configuration. The container runs as UID and GID 10001, so `./data/media` must be owned by that user before the first start.

The full walkthrough, including secret generation, directory ownership, firewall rules, backups, and the diagnostic runbooks, is in [docs/deployment.md](docs/deployment.md).

### From source

Go 1.27, plus `curl`, `jq`, `tar`, and `openssl` on the path.

```bash
make build
```

The build vendors the pinned frontend assets into `web/vendor/`, verifies each download against the digest its publisher signed, compiles the Tailwind stylesheet, and produces a static `./isane` with the whole frontend embedded. No page in this application ever makes an external request, so every library and font is fetched at build time and compiled into the binary.

## Usage

The first start against an empty database prints a one-time invite URL to stdout and to the log. Opening it creates the first account with admin rights, and everything after that happens in the browser.

There is no open signup and no email delivery. An admin creates an invite from `/admin`, which displays its URL once, and hands it over out of band. Password resets go the same way, which is the correct trade for a team of twenty.

**Agents.** An admin reserves a handle from `/admin`, which displays a claim token once. The owner's daemon registers with that token from their own machine, and the agent moves to serving. Mentioning `@handle` dispatches a job carrying the triggering message, and the answer comes back as an ordinary message in the channel. An agent sees only the message that mentioned it unless it registered with history allowed.

**Calls.** There is one call type. Turning a camera off already stops the capture track and publishes no video, so an audio call costs no video bandwidth without being modelled as a separate thing.

**Local development.** `make run` serves `http://localhost:8080` with no certificate at all. `localhost` is a secure context by definition, so service workers and push subscriptions work there with no TLS setup. They do not work against a self-signed certificate on an IP address, which is why `server.insecure` is a development affordance and never a deployment mode.

Configuration lives in `config.yaml`, and every scalar key may be overridden by an environment variable named by uppercasing the YAML path and joining the segments with underscores, so `push.vapid_private_key` becomes `PUSH_VAPID_PRIVATE_KEY`. `config.example.yaml` carries every key and [docs/deployment.md](docs/deployment.md) documents the defaults.

## Notes

- **Web Push on iOS requires the site to be added to the home screen.** It does not work from a Safari tab, so the application shows a one-time instruction card when it detects iOS Safari not running standalone.
- **An iOS home-screen web app cannot hold a call when it is backgrounded or the screen locks.** The outgoing microphone stops. Calls work while the app is in the foreground, and no web technology works around this.
- **Screen share does not exist in any mobile browser.** `getDisplayMedia` is undefined there, so the control is hidden on small viewports rather than failing when it is tapped.
- **A call start is a notification, not a ring.** Nothing on the web can produce a ringing incoming call on iOS, and attempting one produces a worse result than a clear notification.
- **Recordings are per-speaker audio files**, one per participant, written to disk for later transcription rather than composed into a single video.
- **Deleting a message keeps its row.** The body is blanked and the sequence number survives, so clients holding a cursor never see a hole.
