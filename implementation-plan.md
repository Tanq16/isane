# Implementation plan

Twenty-one defects reported against the live deployment at https://isane.etheriosking.com, grouped into backend and frontend work that can proceed in parallel. Every file appears in exactly one of the two sections.

## Decisions already taken

These were settled before the plan was written. Do not re-open them.

- **Channels keep their `mentions` notification default.** Fixing the push suppression bug does not, on its own, make ordinary channel messages notify. A channel notifies on every message only after its owner raises it to All in the channel settings panel. DMs notify on every message by default.
- **Clicking the New pill marks the whole container read.** The divider goes for good, the sidebar unread count clears, and the read marker advances to the container's newest sequence.
- **The divider anchors on a real read marker.** `ContainerView` gains `last_read_seq`, because `last_seq - unread` lands in the wrong place whenever thread replies or deleted messages occupy sequence numbers inside the unread window.
- **A DM gets the same three-level radio a channel has**, not a binary mute toggle. Mentions-only is useful in a busy group DM, and the level machinery already exists.
- **The call panel never carries a join control.** Joining moves to the call system message in the timeline.
- **Closing the call panel hides it and keeps the call running.** Leaving already has its own control.

## Contracts between the two agents

Three JSON field names cross the boundary. They are fixed here so neither agent has to guess.

| Field | Carried on | Written by | Read by |
|---|---|---|---|
| `call_id` | a message object | backend | frontend |
| `last_read_seq` | a container view object | backend | frontend |
| `visible` | the socket `hello` payload, and a new `visibility` frame | frontend | backend |

The `visibility` frame is a client-to-server frame carrying `{"visible": bool}`. The client sends it on every `visibilitychange`, and sends `visible` in the opening `hello` as well.

Only one database migration is added, `internal/store/migrations/0003_message_call_id.sql`. Nothing else in this plan needs one.

---

# Backend

## B1. Push is suppressed for every device whenever any session is online

`internal/push/router.go:188-190` drops every subscription the user owns when no live socket reports a matching push endpoint:

```go
if !matched && r.presence.Online(userID) {
    return nil, nil
}
```

`matched` is false on every page load, because the endpoint reaches the server only in the opening `hello` frame while `currentEndpoint()` is still null: `internal/server/static/js/main.js:441` calls `connect()` before `initPush()` at `:444`. One open desktop tab therefore silences the account, the phone included.

The suppression must become per device and must key on whether that device's page is visible, not on whether a socket exists.

- Delete the blanket bail at `internal/push/router.go:188-190`.
- Keep the deliberate skip at `internal/push/router.go:179-182`, but narrow its condition to a subscription whose device currently reports a visible page.
- `internal/socket/conn.go`: store a visibility flag beside the push endpoint, defaulting to visible.
- `internal/socket/frame.go:68`: accept `visible` on the hello payload, and add a `visibility` frame type the client can send at any time.
- `internal/socket/hub.go:219-224`: record the flag; add a lookup that answers whether a given endpoint is both live and visible.
- `internal/socket/hub.go:114-118`: bound `Online` by the same freshness `EndpointLive` already applies at `internal/socket/conn.go:79-81`, so a socket that is dead but not yet reaped stops suppressing pushes for up to `pongWait`, which is 65 seconds.

A locked Android phone is the same defect. Chrome keeps answering the protocol ping, so `touch()` at `internal/socket/conn.go:87-90` keeps the socket fresh while the page renders nothing, and the phone's own subscription is skipped as live.

`send.go` needs no change. `pushTTL` is already 86400 at `internal/push/send.go:21` and `Urgency` is already `UrgencyHigh` at `:45`, both the maximum useful values.

## B2. A push with no text renders an empty body

`internal/push/payload.go:38` builds the preview from `markdown.Truncate(markdown.PlainText(m.Body), bodyLimit)`. An attachment-only message yields an empty string, so a DM push shows a blank body and a channel push shows a bare display name.

Fall back to an attachment count when the plain text is empty. Backend only; `internal/server/static/sw.js:28` already renders whatever body arrives.

## B3. Mute is refused for a DM and a group DM

Storage is already generic: `notification_prefs` is `(user_id, container_id, level)` keyed on `containers(id)` with no kind constraint (`internal/store/migrations/0001_init.sql:117-122`). Three sites restrict it.

- `internal/server/handlers/containers.go:252-255` returns 400 with `"a notification level applies to channels only"`. Delete the gate.
- `internal/push/router.go:90-92` returns true for a conversation before the level is read. Remove the short-circuit so a conversation runs the same level check a channel does. Removing it also revives DM thread mute, which is inert today because the branch returns before the thread block at `internal/push/router.go:101-110`.
- `internal/push/router.go:138-149` loads the prefs map only for a channel, leaving `rt.prefs` nil for a conversation. Load it for both kinds.

`internal/store/containers.go:40` coalesces an absent row to `'mentions'` for every container. Make the default kind-dependent: `'all'` for a conversation, `'mentions'` for a channel. Without this the radio paints "Mentions only" for a DM the server treats as "All messages".

A group DM and a one-to-one DM are the same `kind = 'conversation'` row throughout this path, so one change serves both.

## B4. The divider anchors on arithmetic that is wrong

The client computes `sentSeq = last_seq - unread`. `unread` counts only top-level, non-deleted messages (`internal/store/containers.go:36-39`) while `last_seq` counts everything, so the divider lands below its true position, or on a thread reply that never renders.

- `internal/store/models.go:159-165`: add `LastReadSeq int64` to `ContainerView`, serialised as `last_read_seq`.
- `internal/store/containers.go`: select the read marker in `containerViewSQL` alongside the existing left join, coalescing an absent marker to 0.

The value is already stored; `internal/store/readstate.go:12-23` writes it and clamps to the container's `last_seq`.

## B5. A call system message carries no link to its call

`internal/server/handlers/calls.go:78` posts the body `"@"+u.Handle+" started a call"` with no identifier, and `store.Message` has no field pointing at a call, so the timeline cannot offer a join control on it.

- New `internal/store/migrations/0003_message_call_id.sql`:
  ```sql
  alter table messages add column call_id uuid null references calls(id) on delete set null;
  ```
- `internal/store/models.go:167-185`: add `CallID *uuid.UUID` with tag `json:"call_id,omitempty"` to `Message`, and the same to `NewMessage` at `:186`.
- `internal/store/messages.go`: add `call_id` to `messageCols` (`:13-14`), `messageColsQualified` (`:16-17`), `scanMessage` (`:23-28`), and the insert column and value lists (`:58-60`).
- `internal/app/publish.go`: add `PostCallNotice` beside `PostSystem` (`:101-119`) that sets `CallID`. Leave the two existing `PostSystem` callers at `internal/app/publish.go:246` and `internal/app/app.go:165` alone.
- `internal/server/handlers/calls.go:78`: post through it with `call.ID`.

Liveness needs no new plumbing. `state.calls` on the client already holds only live calls, seeded from `ReadyPayload.Calls` and maintained by the `call_started` and `call_ended` frames.

## B6. Deployment configuration

`rtc.advertise_internal_ip: true` switches LiveKit's NAT1-to-1 rewrite from replace mode to append mode, so every enumerated host address survives alongside the public one. LiveKit runs on the host network, so it enumerates the other deployment's Docker bridges too, and offered `172.18.0.1` as an ICE candidate. Both `10.0.1.0/24` and `172.18.0.0/16` are common client LAN ranges, so a browser can waste time pairing against an unrelated device on its own network.

- `livekit.example.yaml`: add `rtc.ips.includes: ["10.0.1.0/24"]`, keeping the one internal address egress needs and dropping the bridges. Interface excludes are the wrong lever, because compose bridge names are `br-<hash>` and change.
- `docs/deployment.md`: document that setting beside the `PUBLIC_IP` paragraph at `:102`, and add the cloud security-list rules the firewall section at `:43-58` does not currently cover. That section documents `ufw` against the repo defaults of `7881` and `50000-50300`, which is neither what this deployment uses nor where the packets are being dropped.

## Backend files

`internal/push/router.go`, `internal/push/payload.go`, `internal/socket/conn.go`, `internal/socket/frame.go`, `internal/socket/hub.go`, `internal/server/handlers/containers.go`, `internal/server/handlers/calls.go`, `internal/store/models.go`, `internal/store/messages.go`, `internal/store/containers.go`, `internal/app/publish.go`, `internal/store/migrations/0003_message_call_id.sql`, `livekit.example.yaml`, `docs/deployment.md`.

---

# Frontend

## F1. Message action icons render as blank boxes

`drawIcons` passes an option the vendored Lucide does not accept:

```js
// internal/server/static/js/ui/dom.js:23-27
if (nodes.length) lucide.createIcons({ nodes })
```

`createIcons` accepts `icons`, `nameAttr`, `attrs`, `root`, and `inTemplates`. `nodes` is discarded, so `root` stays `document` and the call sweeps the live document instead of the subtree it was handed. Rows are built detached (`internal/server/static/js/ui/message.js:296` draws into `article` before `internal/server/static/js/ui/timeline.js:190` inserts it), so the sweep triggered by row *i* repairs row *i-1* and misses row *i*. The last row rendered keeps blank icons until some unrelated sweep runs, which is the reported "sometimes".

- `internal/server/static/js/ui/dom.js:23-27`: pass `{ root }`.
- `internal/server/static/js/render.js:279-283` is a second copy of the same broken helper, which blanks callout icons and code copy buttons. Delete it and import `drawIcons` from `./ui/dom.js`. There is no import cycle: `ui/dom.js` imports only `../store.js`.

Every current call site passes a containing element, so tightening the sweep regresses nothing.

While in this file: `internal/server/static/js/ui/sidebar.js:192` writes `entry.chevron.dataset.lucide` on an `<i>` Lucide already replaced and detached, so the group collapse chevron never flips. Re-read the reference after replacement.

## F2. The ellipsis button on a message is redundant

`internal/server/static/js/ui/message.js:193-216` builds an `md:hidden` button opening a modal whose rows come from the same `messageActions` array as the inline bar (`:148-175`), so it cannot contain an action the bar lacks. On desktop `.md\:hidden` renders nothing at all. On touch the bar is already revealed by focus, because `.group-focus-within\/msg\:flex` carries no media query and `article.tabIndex = 0` at `:234` makes tap-to-focus work.

Delete `:193-216`, return `bar` directly instead of the fragment at `:181` and `:218-219`, and drop the now-unused `openModal` from the import at `:5`. `confirmModal` is still used at `:163`.

The inline buttons are `h-7 w-7` at `-top-3 right-4`, so they overlap the row above and are a smaller touch target than the 44px sheet rows. Enlarge the touch target on the mobile breakpoint rather than restoring the sheet.

## F3. Opening a container does not land at the newest message

The scroll is issued and the rows are present when it runs (`internal/server/static/js/ui/timeline.js:386-387` sets `scrollEl.scrollTop = scrollEl.scrollHeight`), but several boxes are not final at that instant and nothing re-asserts the position.

- `internal/server/static/js/ui/timeline.js`, in `mount` around `:465-490`: add a `ResizeObserver` on `listEl` that re-runs the scroll while `atBottom` is true. Keep the existing explicit scrolls.
- `internal/server/static/js/ui/message.js:47-53`: set `video.width` and `video.height` from `a.width` and `a.height`, which the payload already carries. Images already get theirs at `:39-42`.
- `internal/server/static/js/render.js:99-101`: carry `width` and `height` through the markdown image renderer when the token has them, and drop `loading = 'lazy'` inside the timeline, because a lazy image with no dimensions is the worst case for anchoring.

Mermaid renders asynchronously at `internal/server/static/js/render.js:399` and `:411`, the push banner appears up to three seconds later (`internal/server/static/js/main.js:443-449`), and `font-display: swap` reflows rows on a cold load. The observer covers all three; a `requestAnimationFrame` would not.

## F4. The new-message divider never clears

The divider is computed once per container visit at `internal/server/static/js/ui/timeline.js:320-322` and the New pill at `:100-107` is a `<span>`, not a control.

- Make the pill a `<button>`.
- On click: set `dividerSeq = 0`, re-render, and advance read state the way `flushRead` does at `:446-458`, sending the container's `last_seq` over the existing `read` frame. `internal/store/readstate.go:12-23` clamps a too-large sequence, so this is safe. Update `c.unread` and `c.mentions`, then `notify('containers')`.
- Anchor the divider on `c.last_read_seq` from B4 instead of `last_seq - unread`.

No new endpoint is needed; the `read` frame already exists end to end.

## F5. The composer preview keeps its box after a send

`previewEl` is a normal-flow sibling inside the composer rail (`internal/server/static/js/ui/composer.js:432-433`). `submit` empties it at `:318` but leaves `previewOpen` true, so a 32px `bg-base` box stays under a `bg-mantle` pane. `#composer` is `shrink-0` and `#timeline` is `flex-1`, so every pixel the preview holds is a pixel the message list loses. The panel has no height cap, unlike the textarea capped by `autosize()` at `:68-72`.

- Add one `setPreview(open)` owning `previewOpen`, the `hidden` class, `aria-pressed`, the mauve tint, the title, clearing `previewTimer`, and clearing children on close. Route `togglePreview` (`:241-248`) and `renderNotice` (`:341`) through it.
- Replace the `previewEl.replaceChildren()` at `:318` with `setPreview(false)`.
- Add `max-h-64 overflow-y-auto` to the class string at `:432`.

A pending 200ms `previewTimer` from `schedulePreview` (`:233-239`) can otherwise fire after a send and repopulate the panel from an empty textarea. Both composers inherit the fix, since `createComposer` serves the main composer at `:528-542` and the thread composer at `internal/server/static/js/ui/thread.js:282-297`.

## F6. Redundant header controls

- `internal/server/static/js/ui/timeline.js:272`: the at-sign button and the settings button both call `openChannelSettings(c)` with the same argument, and the function takes no section parameter. Delete the button and the now-unused `LEVEL_ICON` constant at `:12`. The cost is that `all` and `mentions` become visible only inside the modal; a muted channel keeps its dimmed sidebar row at `internal/server/static/js/ui/sidebar.js:153`.
- `internal/server/static/js/ui/timeline.js:275`: the header search button calls `focusSearch` at `:228-231`, which focuses the sidebar's one search input. It is also broken when the sidebar is collapsed, because `--sidebar-width` goes to `0px` while the input keeps taking focus invisibly. Delete the button and `focusSearch`.

## F7. The DM panel is named for settings but configures nothing

One call site serves both kinds at `internal/server/static/js/ui/timeline.js:274`, always with the `settings` icon. The conversation branch of `openChannelSettings` (`internal/server/static/js/ui/settings.js:151-159`) renders only `participantsSection` (`:130-147`), a read-only roster. The channel branch is genuinely configurable, so the rename must touch only the conversation arm.

- `internal/server/static/js/ui/timeline.js:274`: pick icon and label by kind. `settings` with `Channel settings` for a channel, `users-round` with `Members` for a conversation. `UsersRound` is present in the vendored bundle.
- `internal/server/static/js/ui/settings.js:152-157`: modal title `Members`, icon `users-round`.
- `internal/server/static/js/ui/settings.js:131`: drop the `People` section label, now the only content of a modal titled Members.

The header already renders a `users` glyph as the container-kind marker at `internal/server/static/js/ui/timeline.js:254`, which is why the control takes `users-round` rather than a second identical glyph.

## F8. A DM cannot be muted

Backend item B3 removes the gate. The frontend supplies the control.

- `internal/server/static/js/ui/settings.js:151-159`: call `notificationSection(body, c)` in the conversation branch. It is already generic over the container object.
- `internal/server/static/js/ui/settings.js:11-15`: the `LEVELS` copy says "No notifications from this channel". Make it container-neutral.
- `internal/server/static/js/ui/timeline.js:271-273`: remove the `c.kind === 'channel'` gate on the level bell so a DM gets the same one-click affordance. `headerSignatureOf` at `:204` already includes `c.level`, so the header repaints on a change.

## F9. Calls

**F9a. Starting a call shows the starter a join prompt for their own call.** `start()` at `internal/server/static/js/ui/call.js:346-365` does auto-join at `:360`, but `join()` renders synchronously at `:283-284` while `joined` is still false, and `renderPrompt` at `:441-454` shows whenever `!joined && call`. Deleting `renderPrompt` fixes this and F9d together.

**F9b. A failed join orphans a live LiveKit room.** The catch at `internal/server/static/js/ui/call.js:303-307` sets `room = null` without calling `room.disconnect()`. If `room.connect()` succeeded and `setMicrophoneEnabled(true)` at `:291` then threw, which on Android means a denied or dismissed mic prompt, the connection stays live, the user still shows as a participant, and `leave()` at `:312-327` can no longer reach it. Disconnect before nulling.

**F9c. The call panel cannot be closed.** `renderHeader()` at `internal/server/static/js/ui/call.js:431-439` appends no close control, and `visible` at `:458` is derived purely from call state, so nothing dismisses it. Below the `xl` breakpoint `#call-pane` is `fixed inset-0 z-30` (`internal/server/static/index.html:56-57`), so a call starting in the channel you are reading covers the whole phone screen with no exit, and pressing Leave leaves the panel full-screen showing a join prompt.

Add a module-level `dismissed` flag, an `x` button in `renderHeader` mirroring `internal/server/static/js/ui/thread.js:198`, an Escape listener on `rootEl` mirroring `internal/server/static/js/ui/thread.js:299-303`, and a clause honouring the flag in the `visible` expression. Clear it in `join()` and in `teardown()` so a fresh call reopens the panel. Closing hides the panel and keeps the call running.

**F9d. The join prompt is shown to everybody.** Remove `renderPrompt` (`:441-454`), its call site (`:463`), and its `promptEl` construction (`:480-481`), and drop the bare `Boolean(call)` term from `visible` at `:458` so the condition becomes `joined || connecting`. This is only safe once F9e exists, because otherwise there is no way to join a call you did not start.

**F9e. Joining from the timeline.** `messageNode` returns early for a system message at `internal/server/static/js/ui/message.js:226-228` and `systemNode` at `:222-224` builds inert text.

Give `systemNode` a call branch that reads `m.call_id` from B5, looks up `state.calls.get(m.container_id)`, and appends a join control when the ids match. Add that liveness to `messageSignature` at `:300-315`, otherwise `internal/server/static/js/ui/timeline.js:109-113` never swaps the node when `call_ended` arrives. For a user already joined but with the panel hidden, the same control reopens the panel.

**F9f. Mobile survivability.** `internal/server/static/js/ui/call.js:516-518` disconnects on `pagehide`, and the SDK does the same by default, so switching apps or locking an Android phone ends the call while `visibilitychange` at `:506-508` only re-acquires the wake lock. Decide `disconnectOnPageLeave` deliberately in `roomOptions()` at `:80-101` and align or drop the app's own listener.

`wire()` at `:211-271` subscribes to none of `room.startAudio()`, `room.canPlaybackAudio`, or `RoomEvent.AudioPlaybackStatusChanged`, all present in the vendored SDK, so a viewer whose browser blocks playback gets silence with no affordance. Subscribe and surface a tap-to-hear control.

`playsinline` is already set at `internal/server/static/js/ui/call.js:153`, and `video.muted = true` at `:154` is correct because remote audio attaches separately at `:222-226`. Neither is a defect.

The total media failure is explained by the missing cloud ingress rules, not by any of the above. These three would bite once the network is fixed.

## F10. Notifications, sound, and the tab badge

**F10a. The page must report its visibility.** Backend item B1 reads it.

- `internal/server/static/js/main.js:441-444`: order `connect()` against `initPush()` so the opening `hello` does not race the endpoint.
- `internal/server/static/js/push.js`: expose an endpoint-ready callback.
- `internal/server/static/js/socket.js:103-109`: send `visible` in `hello`, re-send the endpoint once `initPush()` resolves, and send a `visibility` frame on `visibilitychange`. The listener already exists at `:178-180`.

**F10b. An in-page notification path.** The server deliberately skips a device whose page is visible, and the frontend has no replacement, so a desktop session with the app open is silent by design.

Fire from `onMessage` at `internal/server/static/js/socket.js:328` when the page is hidden or the message belongs to another container. Use `registration.showNotification()`, never `new Notification()`, which throws `TypeError` on Chrome Android and `ReferenceError` on iOS Safari unless installed. Gate it on the container's `level` exactly as the server does, so the page never announces something a push would have suppressed.

**F10c. Android notification fields.** `internal/server/static/sw.js:27-36` passes `body`, `icon`, `badge`, `data`, `tag`, and `renotify`. Add `vibrate: [200, 100, 200]`, `silent: false`, and `requireInteraction` for the desktop case. Never set `silent: true` together with `vibrate`, which is a `TypeError`.

A heads-up banner is not reachable from a web page. Chrome creates one notification channel per origin at Android's `IMPORTANCE_DEFAULT`, which makes a sound but does not produce a heads-up, and channel importance is immutable once created. Only the user can raise it, in Android Settings under Chrome, Notifications, Sites. Put that sentence in the settings panel.

**F10d. A notification sound.** Nothing in the repository plays audio. A service worker cannot: `BaseAudioContext` and `AudioContext` are `[Exposed=Window]`, and `NotificationOptions` has no `sound` member, so the push case is the operating system's to sound and only the in-page case is ours.

Vendor `uisfx@0.4.0`'s `sounds/minimal/notification.mp3`, 4222 bytes. Its audio is CC0-1.0 per the package's own `LICENSE-AUDIO`, and it publishes an npm integrity hash, so it fits the `npm_file` mechanism at `Makefile:42-52` that every other vendored asset already uses.

- `Makefile:7-12`: pin `UISFX_VERSION := 0.4.0`.
- `Makefile:60-64`: one `npm_file` call extracting that single file into `$(VENDOR_DIR)`.
- `Makefile:93-95`: one `verify-assets` entry.
- New `internal/server/static/js/sound.js`: a lazily constructed, reused `Audio` element. Handle the rejected `play()` promise, because a browser may refuse audio before the user has interacted with the origin.
- `internal/server/static/js/ui/settings.js:389`: a mute toggle.

Kenney's CC0 pack was rejected because it ships as an unversioned zip with no published checksum and cannot be pinned. Web Audio synthesis was rejected because it does nothing for the push case and reads as a beep rather than a bell.

**F10e. Red count on the tab favicon.** Per-container counts exist on `state.containers` (`internal/server/static/js/store.js:6`), maintained at `internal/server/static/js/socket.js:335-337` and `:370-371`. There is no total, no `document.title` mutation, and no favicon code.

The Badging API cannot do what was asked: it targets installed applications, not browser tabs, and `setAppBadge` is `version_added: false` on Chrome Android, so the existing call at `internal/server/static/sw.js:40-47` is already a silent no-op on the reporter's phone. Keep it for installed desktop and iOS, and add a canvas favicon for the tab.

- `internal/server/static/js/store.js`: a derived total, weighting mentions above plain unread the way `internal/server/static/js/ui/sidebar.js:173-176` already does.
- New `internal/server/static/js/badge.js`: draw `icon-192.png` to a 32x32 canvas, stamp a red disc using the existing `--ctp-red` token, `toDataURL('image/png')`, assign to the icon link, and set `document.title`. The title works in every browser, including ones that ignore an icon update.
- `internal/server/static/index.html:8`: that link declares `type="image/svg+xml"` and cannot hold a PNG data URL. Give it a stable id and swap the type, or append a second link, since the HTML specification takes the last equally appropriate icon in tree order.

## Frontend files

`internal/server/static/js/ui/dom.js`, `internal/server/static/js/render.js`, `internal/server/static/js/ui/message.js`, `internal/server/static/js/ui/timeline.js`, `internal/server/static/js/ui/composer.js`, `internal/server/static/js/ui/settings.js`, `internal/server/static/js/ui/call.js`, `internal/server/static/js/ui/sidebar.js`, `internal/server/static/js/socket.js`, `internal/server/static/js/push.js`, `internal/server/static/js/main.js`, `internal/server/static/js/store.js`, `internal/server/static/js/sound.js` (new), `internal/server/static/js/badge.js` (new), `internal/server/static/sw.js`, `internal/server/static/index.html`, `Makefile`.

`internal/server/static/css/app.css` is generated and gitignored, so any new Tailwind utility exists only after `make assets`.

---

# What only the account owner can do

No code change substitutes for these, and calls cannot connect until they are done.

Add three ingress rules to the security list on the instance's VCN subnet, sourced from `0.0.0.0/0`:

| Protocol | Port | Carries |
|---|---|---|
| TCP | 7891 | LiveKit ICE/TCP for this deployment. 7881 belongs to the separate Element instance on the same host |
| UDP | 3478 | LiveKit's built-in TURN |
| UDP | 60001-60300 | Media 60001-60200 and TURN relay allocations 60201-60300 |

Do not open 7890. Confirm the rules landed on the security list the instance's subnet actually uses, then verify from outside that TCP 7891 accepts a connection and that a STUN binding request to `132.145.199.102:3478` is answered.

The host's own iptables INPUT chain is already ACCEPT with no rejecting rule, so the host firewall is not involved.

# Out of scope

- Live propagation of a notification level change to a user's other sessions. No frame carries it today, so a change reaches another tab only on reconnect. This gap already exists for channels and is a separate defect.
- Setting the RFC 8030 `Topic` header to collapse queued pushes for a phone that has been offline for hours.
- `GET /api/messages/{id}`, without which a reply quote whose parent is outside the loaded window still shows no author and no jump target.
- The push subscription endpoint takeover at `internal/store/push.go:26`, where the upsert keys on endpoint alone.
- `internal/server/static/js/ui/thread.js:137` focuses the first article in the timeline when a thread closes, scrolling the timeline to the top of the loaded window.
- `internal/store/readstate.go:82-91` `NotificationPref` has no callers.
- `internal/server/static/js/ui/sidebar.js:394-401` listens for three events nothing dispatches.
