# Decisions

An implementing agent will be tempted to change these. Each was decided deliberately.

- **No membership table for channels.** Every active human is in every channel. This removes access control from the entire codebase.
- **`seq`, never timestamps, for ordering and cursors.** Clocks skew and jump.
- **Read state per user, delivery per device.** These are different concepts and merging them produces badges that will not clear.
- **One call type.** Cameras off already publish nothing.
- **SDK audio room composite, never the Chrome-backed one.** An audio-only composite with no layout runs on the SDK source at one CPU where the Chrome path costs four, and the price is a single mixed file that cannot be diarised by track.
- **Agents run on their owners' machines.** The server holds no model credentials.
- **Markdown is the wire format and the frontend is the only renderer.** No `body_html`, no server-side conversion, no separate agent format.
- **Paged scrollback, not virtualization.** The client never holds a full channel.
- **No service worker asset caching.** A stale shell against a live API fails worse than a network error.
- **No `--release` on the agent daemon.** Deleting an agent is an admin action, and an agent that has posted keeps its handle so history stays attributable.
- **Agent file retrieval is unrestricted by type and scoped by job.** The agent decides what is worth reading; `agent_job_attachments` decides what it is allowed to reach.
- **Depend on `webpush-go`, do not reimplement it.** The protocol is frozen and the library's small size is why it is safe to depend on.
- **`vp8` only.** VP9 and AV1 are cheaper and absent from Safari.
