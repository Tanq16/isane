# Implementation plan

Nine changes across notifications, unread state, typing, calls, and call recordings. Every claim below carries the `file:line` that proves it.

The work splits cleanly. Backend owns Go, SQL, `egress.example.yaml`, and `docs/`. Frontend owns everything under `internal/server/static/`. The wire contract in each section is fixed, so both halves are written against it rather than against each other.

## 1. No notification sound when the tab is hidden

`announce()` returns before `play()` whenever the page is hidden, at `internal/server/static/js/socket.js:398`; `play()` is one line below at `:402`. Commit `9ba8d3d` widened a guard that had covered only the notification banner and took the sound with it.

A service worker cannot make the sound. `HTMLAudioElement` is `[Exposed=Window]` per the HTML specification and `AudioContext` is `[Exposed=Window]` per the Web Audio specification, so neither exists in `ServiceWorkerGlobalScope`. `silent: false` at `internal/server/static/sw.js:33` already does everything it can; the Notifications specification delegates the sound to the platform, and on macOS that is the Chrome-wide setting in Notification Center, not addressable per site. A hidden but loaded tab may play audio: Chrome's autoplay rules gate on a prior click in the document, not on visibility, and Chrome's page-lifecycle rules exempt a tab that plays audio or updates its favicon from freezing, which is why the badge at `internal/server/static/js/badge.js:41-48` already moves.

**Frontend, `internal/server/static/js/socket.js`, `announce()`.** Move `play()` above the visibility gate and keep `showNotification` below it:

```js
if (!container) return
if (state.me && m.author_id === state.me.id) return
if (pageVisible() && state.current.containerId === m.container_id) return
if (!shouldAnnounce(container, m)) return
play()
if (!pageVisible()) return
// registration / showNotification block unchanged
```

This cannot reintroduce the double-banner regression, because `showNotification` stays behind `pageVisible()` and a hidden device therefore gets exactly one banner, the push one from `sw.js:42`. It cannot double the sound either: a visible tab already pairs `play()` with a `silent: false` notification today. `sw.js` is not touched.

## 2. Mark as read on open

The preference is account-level, stored on `users`, delivered on the socket ready payload. Read markers are already server-side and shared, so a device-local flag would let the phone auto-mark read while the laptop did not.

Threads have no unread state at all: `onMessage` skips the unread counter for a message carrying `thread_root_id` (`internal/server/static/js/socket.js:350`), `UnreadCounts` filters `m.thread_root_id is null` (`internal/store/readstate.go:38-40`), and `read_markers` is keyed on container only (`readstate.go:12-23`). The setting therefore governs channels and DMs. Do not add per-thread read state in this change.

### Backend

1. `internal/store/migrations/0004_user_mark_read_on_open.sql`, one statement:
   `alter table users add column mark_read_on_open boolean not null default true;`
2. `internal/store/models.go`: add `MarkReadOnOpen bool \`json:"mark_read_on_open"\`` to `User` (`models.go:90-101`). Not to `DirectoryUser`; it is private to its owner.
3. `internal/store/users.go`: append `mark_read_on_open` to `userColumns` (`users.go:11`) and `&u.MarkReadOnOpen` to `scanUser` (`users.go:17-22`). Those are the only two places a user row is read. Add `SetMarkReadOnOpen(ctx, id uuid.UUID, on bool) error` beside `UpdateProfile` (`users.go:134-137`) using `db.execOne`, and leave `UpdateProfile`'s signature alone.
4. `internal/server/handlers/auth.go`: add `MarkReadOnOpen *bool \`json:"mark_read_on_open"\`` to `profileRequest` (`auth.go:62-65`), and in `UpdateProfile` (`auth.go:133-170`) call `SetMarkReadOnOpen` when the pointer is non-nil, before the `GetUser` re-read at `auth.go:163`.

No new endpoint and no new socket frame. `ReadyPayload.User` is a full `store.User` (`internal/app/handler.go:18,75-76`) assigned to `state.me` at `socket.js:279`, and `PATCH /api/auth/me` already returns the updated user.

### Frontend

`internal/server/static/js/ui/settings.js`:

- Extract the toggle body shared by the new row and `soundRow()` (`settings.js:390-412`) into `toggleRow(body, label, isOn, onChange)` and rewrite `soundRow` to call it. Two real consumers, so this is DRY rather than speculation.
- Add `readingSection(body)`: the toggle labelled "Mark as read on open", plus a `?` button in `text-xs text-overlay1` revealing one help line, "Turn this off and you have to click the New badge to mark messages as read." The toggle calls `api.patch('/api/auth/me', { mark_read_on_open: next })`, then sets `state.me` and `notify('me')`, mirroring `profileSection`'s save at `settings.js:313-319`.
- Call it from `openUserSettings()` (`settings.js:474-496`), between `pushSection(body)` and the Session label.

`internal/server/static/js/ui/timeline.js`:

- Add `const DIVIDER_LINGER = 4500` beside `NEAR_BOTTOM` (line 11) and `let dividerTimer = 0` beside `dividerSeq` (line 32).
- In `render()`'s container-switch branch (`:307-323`), `clearTimeout(dividerTimer)` and set the anchor by mode. On keeps today's line, `dividerSeq = c && (c.unread || 0) > 0 ? sentSeq : -1`. Off sets `dividerSeq = c ? sentSeq : -1` unconditionally, so a message arriving while the container is open still raises a marker the user can click.
- New `markOnOpen(cid)`, called at the end of both non-jump paths of `openContainer()` (after the `render()` at `:369`, and at `:383`), guarded on `cid === state.current.containerId` and `dividerSeq >= 0`: call `markRead(cid, c.last_seq || 0)`, the same value `dismissDivider` uses at `:466`, then start `dividerTimer` to clear `dividerSeq` and re-render after `DIVIDER_LINGER`, re-checking that the container has not changed. Plain removal, no transition class. Starting the timer after the load rather than at switch time is what guarantees the marker is on screen for the full interval.
- Guard `flushRead()` (`:454`) with an early return when the preference is off. That single guard covers both entry points, the observer at `:496` and the `visibilitychange` listener at `:511-513`, leaving the New pill as the only way to advance the marker.
- Exclude the `?m=` deep-link branch of `openContainer()` (`:351-365`). A jump lands the reader mid-history and marking the whole container read there would be wrong.

## 3. Typing indicators

Nothing clears a typing entry except its own expiry. `onTyping` stamps `Date.now() + TYPING_TTL` with `TYPING_TTL = 5000` (`socket.js:11,450`), the sender emits at most once per `TYPING_INTERVAL = 3000` (`composer.js:7,271-278`), and `pruneTyping` runs at `TTL + 100` (`socket.js:452`). The last frame lands up to 3000 ms before the send and expires 5000 ms after that, plus up to 1000 ms of sweep, so the indicator survives the message by two to six seconds. `onMessage` (`socket.js:344-366`) never touches `state.typing`.

Clear on arrival rather than adding an explicit stop frame. It costs zero frames on the wire and fixes the reported case exactly; an explicit stop would add a client frame plus a fan-out per member on every send. `TYPING_TTL` and `TYPING_INTERVAL` stay as they are, because a TTL below the interval plus jitter makes a continuously typing user flicker, and the TTL now covers only the abandoned draft.

Two further defects in the same area are in scope. `lastTyping` is never reset on send (`composer.js:49,280-337`), so the first keystrokes of the next message emit nothing for up to three seconds. And the typing frame carries only a container id (`internal/socket/frame.go:100-112`) while the thread composer's `target()` returns the thread's container id (`thread.js:287-291`), so typing in the main timeline shows "X is typing" in the thread pane and the reverse.

### Backend

`internal/socket/frame.go`: add `ThreadRootID *uuid.UUID \`json:"thread_root_id,omitempty"\`` to both `TypingPayload` (`:100-102`) and `TypingEventPayload` (`:109-112`). Carry it through `App.Typing` (`internal/app/handler.go:142-153`) and the `typing` case in `internal/socket/conn.go` unchanged in every other respect.

### Frontend

- `internal/server/static/js/socket.js`, in `onMessage()` (`:344`), before `notify(...)`: delete the author's entry for that container and `notify('typing')` when the delete removed something, dropping the container key when the map empties.
- `internal/server/static/js/ui/composer.js`: set `lastTyping = 0` in `submit()` where the textarea is cleared (`:313`); send `thread_root_id` on the outgoing frame from `maybeTyping()` (`:271-278`) taken from `target()`; and filter in `renderTyping()` (`:123-139`) on both the container id and the thread root id so the two composers stop showing each other's typists.
- `internal/server/static/js/socket.js`, `onTyping` (`:443`): key the inner map on the user and record the thread root alongside the expiry so the composer can filter.

## 4. Recording state reaches every participant

`Recording()` writes the state and returns it in the HTTP body only (`internal/server/handlers/calls.go:146-151`), with no broadcast, unlike `Start()` at `:77`. No frame type carries it (`internal/socket/frame.go:24-40`), so the client compensates locally at `internal/server/static/js/ui/call.js:404`. The webhook paths that move the state are equally silent: `eventRoomFinished` writes `RecordingProcessing` at `:209` and `finishEgress` writes `RecordingReady` at `:261`.

A second defect sits in the same handler. The manual stop path writes `RecordingOff` (`:132,146`) while `finishEgress` promotes to `ready` only from `RecordingProcessing` (`:254-256`), so a recording stopped with the Record button never reaches `ready`, and the client's optimistic `'processing'` is a value the server never wrote.

### Backend

1. `internal/socket/frame.go`: add `TypeCallRecording = "call_recording"` to the outbound const block (`:24-40`) and, beside `CallEndedPayload` (`:137-139`):

```go
type CallRecordingPayload struct {
	CallID uuid.UUID            `json:"call_id"`
	State  store.RecordingState `json:"recording_state"`
}
```

`store` is already imported at `frame.go:10`. Keep the payload narrow rather than rebroadcasting the whole `store.Call`, which would re-seed `participants` and race the `call_participant` frames.

2. `internal/server/handlers/calls.go`: add `setRecording(ctx, call store.Call, state store.RecordingState) error`, which calls `SetRecordingState` then broadcasts the frame to the container. Route all three writers through it: `Recording()` at `:146`, `eventRoomFinished` at `:209`, `finishEgress` at `:261`.
3. Same file: `stopEgress` (`:324-335`) returns the number of egresses it stopped, and `Recording()`'s off branch picks `store.RecordingProcessing` when that count is above zero and `store.RecordingOff` otherwise, so `finishEgress` can promote to `ready`. Drop the local `state` variable at `:132` in favour of a value computed per branch.

### Frontend

- `internal/server/static/js/socket.js`: add `case 'call_recording'` beside `call_participant` (`:238-240`), handled by an `onCallRecording(d)` modelled on `onCallParticipant` (`:481-491`) that finds the matching call in `state.calls`, sets `recording_state`, and calls `notify('calls')`.
- `internal/server/static/js/ui/call.js`: delete the optimistic write at `:404`. `toggleRecording` writes the handler's response body into `state.calls` and calls `notify('calls')`; the broadcast frame then lands as a no-op for the initiator.

## 5. A call ends when its last participant leaves

The only two `EndCall` call sites are the `room_finished` webhook branch (`internal/server/handlers/calls.go:213`) and the reaper (`internal/app/app.go:206`). `Leave()` (`:110-115`) and `eventParticipantLeft` (`:193-201`) broadcast a participant frame and stop, neither counting participants.

The delay is LiveKit's departure timeout: `livekit.example.yaml:38` sets `departure_timeout: 20`, documented as the number of seconds to keep the room open after everyone leaves in `livekit_room.proto:88-89` at `github.com/livekit/protocol@v1.51.0`. The reaper cannot beat it, running at `callReapInterval = time.Minute` (`internal/app/app.go:29`) and skipping any call whose LiveKit room still exists (`:198-205`). Client-side the Join affordance keys purely on `state.calls` holding a matching id (`internal/server/static/js/ui/message.js:198-207`), so only `call_ended` clears it.

Fix it server-side rather than by lowering `departure_timeout`, which is deployment-only, does nothing for a running deployment, and breaks a legitimate reconnect when set near zero.

### Backend

1. `internal/store/calls.go`: add `EndCallIfEmpty(ctx, id uuid.UUID) (bool, error)`, modelled on `EndCall` (`:144-162`), running inside `db.Tx` with the update guarded so two concurrent leaves cannot both win:

```sql
update calls set ended_at = now()
where id = $1 and ended_at is null
  and not exists (select 1 from call_participants where call_id = $1 and left_at is null)
```

Return `tag.RowsAffected() == 1` and keep `EndCall`'s `call_participants` sweep in the same transaction.

2. `internal/server/handlers/calls.go`: add `endIfEmpty(ctx, call store.Call)` calling `EndCallIfEmpty` and, when it returns true, stopping the egress, moving the recording state to `store.RecordingProcessing` through `setRecording` when the call was recording, and broadcasting `socket.TypeCallEnded`. Call it from `Leave()` after the broadcast at `:114` and from `eventParticipantLeft` after the broadcast at `:201`. Both are needed, because `disconnectOnPageLeave: false` (`internal/server/static/js/ui/call.js:77`) means a closed tab never reaches `Leave()`.

3. Deleting the LiveKit room. `DeleteRoom` (`internal/calls/service.go:65-74`) is referenced nowhere today, and leaving a room alive lets a six-hour access token (`internal/calls/token.go:12`) rejoin a call the database has ended. Call it, but settle the ordering against an in-flight recording first: read the egress source for what a `DeleteRoom` does to a room composite egress that has not finalised, and if stopping the egress and deleting the room in the same breath can truncate the file, defer the delete until `egress_ended` arrives for that call. Log and continue on a `DeleteRoom` error, since `Leave()` is not gated on `h.enabled(w)` and `authorize` returns `ErrDisabled` when LiveKit is off (`internal/calls/service.go:77-79`).

A late `room_finished` for an already-ended call is harmless: `EndCall`'s `coalesce(ended_at, now())` (`internal/store/calls.go:147`) still matches the row, and `CallByRoom` then reports `processing`, so the recording branch skips. Leave the reaper (`internal/app/app.go:185-213`) alone as the backstop for a lost webhook.

4. Scope the live-call list. `ListLiveCalls` (`internal/store/calls.go:96-98`) selects every row with `ended_at is null`, so every user's ready payload carries the room name, starter, and participant ids of every live call in the deployment, including containers they are not a member of. Take the user id and filter to channels plus conversations the user participates in, mirroring the membership rule in `MemberIDs` (`internal/store/containers.go:255-275`), and update the single call site at `internal/app/handler.go:50`.

### Frontend

`internal/server/static/js/socket.js`: `onReady` currently only adds to `state.calls` (`:292-296`), so a call that ended while the socket was down leaves a permanent stale entry lighting both the sidebar indicator and the Join button. With the payload now authoritative, rebuild the map instead:

```js
const live = new Map()
for (const call of d.calls) {
  if (call && call.container_id && !call.ended_at) live.set(call.container_id, call)
}
state.calls = live
```

That invalidates every object reference held elsewhere, which is why the call panel reads through `liveCall()` in section 7 rather than holding `activeCall` directly.

## 6. The header call icon

`renderHeader()` renders one `iconButton('phone', 'Start a call', ...)` for every non-archived container (`internal/server/static/js/ui/timeline.js:266-270`), its appearance never varying with call state, and it dispatches `isane:call-start`, which joins. `headerSignatureOf` (`:206-210`) carries only `call.id`, so the header does not repaint when membership or panel state changes.

**Frontend only.** Three cases:

| State | Control |
|---|---|
| No live call | `iconButton('phone', 'Start a call', ...)` dispatching `isane:call-start`, unchanged |
| Live, this tab not joined | An inert `phone-call` indicator in `text-teal`, matching the sidebar indicator at `sidebar.js:170`. It does not join; the Join control stays in the timeline |
| Live, this tab joined | `iconButton('phone-call', 'Show the call', () => call.reopen())` |

Import `* as call from './call.js'`; there is no cycle, since `call.js` imports only `../store.js`, `../api.js`, `../socket.js`, and `./dom.js`. Extend `headerSignatureOf` with the live call's `recording_state` and with `call.panelState().joined` and `.hidden`, or the header will not repaint.

For the teal tone, do not append a colour class through `iconButton`'s `extra` argument: the base class string at `internal/server/static/js/ui/dom.js:29` already carries `text-overlay1`, and two conflicting `text-*` utilities resolve by generated-CSS order rather than by attribute order. Build the node, then `classList.remove('text-overlay1')` and `classList.add('text-teal')`.

## 7. The call panel: icons, close, expand, resize

`controlButton()` builds a text-labelled button (`internal/server/static/js/ui/call.js:412-422`) and `renderControls()` emits five of them (`:424-436`), with no `aria-pressed`, no `title`, and no `aria-label`. The panel is `xl:w-[380px]` with no custom property (`internal/server/static/index.html:57`), so it cannot be dragged; `render()` hides it whenever a thread is open (`:456`); nothing reopens it once `dismiss()` sets the module flag (`:450-453`); and the tile grid's `sm:grid-cols-2` (`:495`) is a viewport query, so a 380px docked pane renders two 180px tiles on a wide screen.

The vendored bundle is `lucide v1.38.0` (`internal/server/static/vendor/lucide.min.js:2`, pinned at `Makefile:11`). `drawIcons(root)` calls `lucide.createIcons({ root })` (`dom.js:23-26`), which matches descendants only and never the root element, so it must run after the nodes are appended.

Every class named here is absent from the committed `internal/server/static/css/app.css` until `make assets` runs, and that file is gitignored, so it is a build step and not a commit.

### Frontend

**A shared resize helper, `internal/server/static/js/ui/resize.js`.** Three real consumers exist, so this is DRY rather than speculation: the sidebar's `wireSidebarResize()` (`main.js:88-128`), and the two right-hand panes. Export `wireResize({ handle, pane, edge, prop, key, min, max, fallback })` returning `{ read, apply }`, where `edge` is `'left'` or `'right'` and `max` may be a number or a function. Width comes from the pane's own bounding rect rather than from `clientX` against `innerWidth`, which is what makes a right-edge pane correct when another pane sits beside it. Keyboard arrows move by 16px with the sign flipped for a right edge. Only `main.js` imports it; the helper knows nothing about calls, threads, or the sidebar.

**`internal/server/static/index.html`.** Two new handles, siblings in the `#app` flex row, each immediately before its pane, shaped like `#sidebar-resize` (`:44-45`) but gated on `xl`. Pane visibility moves from a JS `hidden` toggle to `#app` data attributes, matching the existing `data-drawer` and `data-sidebar` pattern (`:38`): `data-thread="closed|open"` and `data-call="hidden|docked|expanded"`. The docked utilities then live behind `:is(:where(.group)[data-call=docked] *)` at specificity (0,2,0) against the base `fixed`/`hidden` at (0,1,0), so docked wins and expanded loses without depending on utility source order. `#call-pane` gains `border-l border-surface0` when docked, matching every other divider in the app (`index.html:48,54`; `thread.js:171`); without it, `bg-crust` against `#main`'s `bg-mantle` is a 1.07 step and reads as no seam.

Expanded is the mobile layout applied at every width: the pane keeps its base `fixed inset-0 z-30` and never picks up the `xl:` docked overrides. `#main` stays rendered underneath, so the timeline's scroll position survives, where a `display:none` on `#main` would reset it. `z-30` keeps it under `#modal-root` and `#toast-root` (`:64-67`).

**`internal/server/static/css/input.css`.** Two lines in the existing `:root` block so a `var()` that JS never set resolves instead of collapsing to `width: auto`: `--thread-width: 380px;` and `--call-width: 380px;`.

**`internal/server/static/js/main.js`.** Replace `wireSidebarResize()` with `wirePanes()`, three `wireResize` calls. The sidebar's is a straight port with `edge: 'left'`, `min: 200`, `max: 400`, `fallback: 240`, no behaviour change. Both right panes take `edge: 'right'`, `min: 280`, `max: () => paneCap()`, `fallback: 380`, keys `isane-thread-width` and `isane-call-width` in the hyphen form that `isane-sidebar-width` already uses.

`paneCap()` is the whole width policy, and it is what stops the old 96px failure repeating:

```
open   = (thread docked ? 1 : 0) + (call docked ? 1 : 0)
budget = innerWidth - sidebarWidth - (open + 1) * 4 - 400
cap    = max(280, min(560, budget / max(open, 1)))
```

`sidebarWidth` must be read from the live computed custom property, not from `read()`, because `toggleSidebar()` writes `0px` directly (`timeline.js:226`), below the minimum. `fitPanes()` re-applies `read()` for each open pane through that cap without overwriting the stored preference, so a pane returns to its chosen width when space comes back; it runs on `window.resize` coalesced through `requestAnimationFrame`, during a drag, and from `onStateChange` (`:272-286`) when the keys include `current` or `calls`. It derives open state from `state.current.threadRootId` and `call.panelState()` rather than from the DOM. At 1280px with both panes open the message column holds at 400px and each pane caps at 314px; at 1536px both sit at 380px and the column gets 524px.

`internal/server/static/js/boot.js` keeps its own copy of the sidebar numbers untouched: it is a classic script in `<head>` that must run before first paint and cannot import from the module graph. It needs no seeding for the two new properties, because both panes start hidden and `input.css` supplies the default.

**`internal/server/static/js/ui/timeline.js`.** Delete `storedWidth()` (`:212-219`) and have `toggleSidebar()` (`:226`) use the `read()` returned by the sidebar's `wireResize`, removing the second copy of the 200/400/240 constants.

**`internal/server/static/js/ui/thread.js`.** `render()` (`:222-229`) sets `#app`'s `data-thread` instead of toggling `hidden` on the pane. Nothing else changes.

**`internal/server/static/js/ui/call.js`.**

- Add a module `expanded` flag. `render()` (`:455-466`) sets `#app`'s `data-call` to `hidden`, `docked`, or `expanded` instead of toggling `hidden`. Every transition of `joined`, `connecting`, `dismissed`, or `expanded` calls `notify('calls')`, because the store is the only repaint trigger the header has (`timeline.js:524`).
- Add `liveCall()` returning `state.calls.get(activeCall.container_id)` when its id matches, and `activeCall` otherwise. Read `recording_state` through it, so the panel is correct whether the frame handler mutates the map entry in place or the ready rebuild replaces it.
- Replace `controlButton()` with `callButton(name, label, tone, onClick, pressed)`. It uses `icon()` from `dom.js:15-21` but not `iconButton()`, whose `h-8 w-8` is a header size rather than a touch target. Shared classes: `grid h-11 w-11 shrink-0 place-items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-mauve`, icon at `h-5 w-5`, plus `title`, `aria-label`, and `aria-pressed` on the four toggles.

| Tone | Classes | Used by |
|---|---|---|
| `on` | `bg-surface0 text-text hover:bg-surface1` | mic live, camera live |
| `off` | `bg-surface0 text-overlay1 hover:bg-surface1 hover:text-text` | camera off, screen idle, record idle |
| `alert` | `bg-red/15 text-red hover:bg-red/25` | mic muted, recording live |
| `accent` | `bg-mauve text-crust hover:brightness-110` | screen sharing live |
| `danger` | `bg-red text-crust hover:brightness-110` | leave |
| `busy` | `bg-surface0 text-overlay0 cursor-not-allowed` | recording processing |

Solid red marks the one destructive action and tinted red marks a hot state.

- `renderControls()` becomes `flex shrink-0 items-center justify-center gap-2 border-t border-surface0 px-3 py-2`, with `drawIcons(controlsEl)` after the buttons are appended.

| Control | Icon | Tone | Label |
|---|---|---|---|
| Mic | `mic` / `mic-off` | on / alert | Mute / Unmute |
| Camera | `video` / `video-off` | on / off | Turn the camera off / on |
| Screen share | `screen-share` / `screen-share-off` | accent / off | Stop sharing / Share your screen |
| Record | `circle-dot` / `circle-stop` / `loader-circle` | off / alert / busy | Start recording / Stop recording / Saving the recording |
| Leave | `phone-off` | danger | Leave the call |

Screen share stays behind `screenShareSupported()` (`:52-54,430`). Record reads `liveCall().recording_state`: `recording` gives alert plus `circle-stop`; `processing` gives busy, `disabled`, `aria-busy="true"`, and `loader-circle` with `animate-spin`; `off`, `ready`, and `failed` all render idle.

- `renderHeader()` (`:438-448`) keeps the title and the `x` close, and gains an expand toggle before the close: `iconButton(expanded ? 'minimize-2' : 'maximize-2', ..., toggleExpand, 'hidden xl:grid')`. `hidden xl:grid` because below `xl` the panel is already full screen, and `iconButton`'s base carries `grid` so the extra must restore it. The recording badge (`:442-444`) gains a pulsing dot.
- Escape (`:510-514`) collapses first when expanded and hides only when already docked.
- The tile grid class is chosen in `render()` from `expanded`, both as literal strings so the Tailwind scanner sees them: expanded takes `grid grid-cols-2 gap-2 lg:grid-cols-3 xl:grid-cols-4`, docked takes `grid grid-cols-1 gap-2`.
- Drop `&& !state.current.threadRootId` from the visibility expression at `:456`. A thread and a call may be docked together, which is the point of making both resizable; `paneCap()` is what keeps the message column honest.
- Export `panelState()` returning `{ joined, connecting, hidden, expanded, callId, containerId, recordingState }`, and `reopen()`, which clears `dismissed` and re-renders with no network call, no LiveKit connect, and no join, and is a no-op when this tab is neither joined nor connecting.

Verify every Lucide name against the vendored bundle before using it. A name that does not exist renders nothing.

## 8. Recordings: capture

Today one track egress runs per participant, writing a per-speaker `.ogg` (`internal/calls/egress.go:11-45`), and the node is configured to accept only track egress, every composite cost priced at 1000 against a 2-CPU host (`egress.example.yaml:14-21`, matching the live `~/isane/egress.yaml`).

Audio-only room composite runs on the SDK source, not Chrome. In `livekit/egress` v1.14.1, `pkg/config/pipeline.go:537-543` returns true for a request that is audio-only with an empty `Layout` and `CustomBaseUrl`, and `pipeline.go:231-236` then sets `SourceTypeSDK`. `pkg/stats/monitor.go:285-293` prices it as `SDKAudioRoomCompositeCpuCost` and, because `costs.isWeb` is false, skips the Chrome admission check at `monitor.go:225-227`. Mixing is real: `audiomixer` at `pkg/pipeline/builder/audio.go:389`. Track composite cannot serve, because `TrackCompositeEgressRequest` carries exactly one `AudioTrackId` and one `VideoTrackId` (`livekit_egress.pb.go:4524-4526`).

Switch to one mixed file per stretch. The cost is one recording at a time on this host and the loss of per-speaker separation.

Format is MP4 with AAC, not OGG with Opus: Safari gained Ogg Opus only in 18.4, `audio/mp4` works everywhere, and the README leans on iOS home-screen use (`README.md:73-74`). `OutputTypeMP4` is in `AudioOnlyFileOutputTypes` (`pkg/types/types.go:207-211`), and `faac`, `mp4mux`, and `audiomixer` are all present in the running aarch64 `livekit/egress:v1.14.1` container.

### Backend

1. `internal/calls/egress.go`: replace `StartTrackEgress` and `audioTrackID` with `StartRoomEgress(ctx, roomName, outPath string) (string, error)` issuing `StartRoomCompositeEgress` with `AudioOnly: true`, empty `Layout` and `CustomBaseUrl` (that emptiness is what selects the SDK source), one `EncodedFileOutput` with `FileType: EncodedFileType_MP4`, `Filepath: outPath`, `DisableManifest: true`, and `Advanced` encoding options setting `AudioCodec_AAC` and `AudioBitrate: 48`, overriding the 128 kbps default at `pkg/config/pipeline.go:207-210` for roughly 21 MB per recorded hour. The `RoomRecord` grant, `StopEgress`, and `ListEgress` are unchanged.
2. `internal/media/service.go`: add `RecordingPath(callID, recordingID uuid.UUID) string` returning `filepath.Join(s.RecordingsDir(), callID.String()+"-"+recordingID.String()+".mp4")`, and `OpenRecording(r store.CallRecording) (*os.File, error)` mirroring `Open` (`:186-192`). Flat, with no per-call subdirectory. This is what makes retention able to unlink: `recordings/` is mode 2775 owned by uid 10001, while any subdirectory egress creates is mode 0755 owned by uid 1001, so `sweepRecordings` fails `os.Remove` and skips `DeleteCallRecording` (`internal/retention/retention.go:96-110`), leaving both file and row forever.
3. `internal/retention/retention.go`: delete the `os.Remove(dir)` branch at `:102-104` now that the layout is flat. Keep the `fs.ErrNotExist` tolerance at `:98`.
4. `internal/server/handlers/calls.go`: `Recording()` rejects an on request when the call is already recording; on turns `startEgress` into `startRecording(ctx, call, startedBy)`, which allocates the recording id first, inserts the row with `UserID: &startedBy`, `Mime: "audio/mp4"`, and `StoragePath: h.app.Media.RecordingPath(call.ID, recID)`, then starts the egress at `path.Join(egressOutRoot, call.ID.String()+"-"+recID.String()+".mp4")`. Delete the `startEgress` branch from `participant_joined` (`:190-192`); the mixer subscribes to new publishers itself. An egress that will not start must reach the user as an error from the handler rather than only a log line, because at one CPU per recording a second concurrent recording on this host is rejected.
5. `egress.example.yaml` and the deployed `~/isane/egress.yaml`: add `sdk_audio_room_composite_cpu_cost: 1` under `cpu_cost`, keep `audio_room_composite_cpu_cost: 1000` (it prices the Chrome-backed variant, `monitor.go:287-289`), and rewrite the comment at `:13`, which is now false. `compose.yaml` needs no change.

## 9. Recordings: message, delivery, and player

No route serves recording bytes; `GET /api/calls/{id}/recordings` (`internal/server/router.go:60`) returns metadata and has no frontend caller. Nothing links a recording to a message. `finishEgress` never reads `info.GetStatus()` (`internal/server/handlers/calls.go:224-262`), so `EGRESS_FAILED` and `EGRESS_ABORTED` (`livekit_egress.pb.go:453-454`) are treated as success, and `store.RecordingFailed` (`models.go:87`) is never assigned. A webhook for an unknown egress aborts the handler, because `CompleteCallRecording` goes through `execOne`, which returns `ErrNotFound` on zero rows (`internal/store/users.go:41-43`).

### Backend

1. `internal/store/migrations/0005_recording_message.sql`:

```sql
alter table messages add column recording_id uuid null references call_recordings(id) on delete set null;
create unique index messages_recording_idx on messages (recording_id) where recording_id is not null;

alter table call_recordings add column mime text not null default 'audio/ogg';
alter table call_recordings alter column mime drop default;
```

The link is a nullable `messages.recording_id` beside `call_id`, not the attachment mechanism, which would duplicate `call_recordings` and hand `Media.Delete` a file the app must not own. `on delete set null` leaves the notice text intact when retention removes the recording. The partial unique index doubles as the idempotency guard for a retried `egress_ended`, since `InsertMessage` maps `23505` to `ErrConflict` (`internal/store/store.go:117-118`). The `mime` backfill is correct: existing rows are per-speaker OGG.

2. `internal/store/models.go`: add `CallRecording.Mime string \`json:"-"\``, `Message.Recording *CallRecording \`json:"recording,omitempty"\``, and `NewMessage.RecordingID *uuid.UUID`. `StoragePath` and `EgressID` stay `json:"-"`.
3. `internal/store/calls.go`: `recordingColumns` gains `mime`; update `scanRecording` and the `CreateCallRecording` insert. Replace `CompleteCallRecording` (`:222`) and `SetRecordingSize` (`:227`) with `RecordingByEgressID(ctx, egressID string) (CallRecording, error)` and `FinishCallRecording(ctx, id uuid.UUID, durationMs *int, sizeBytes int64) error`.
4. `internal/store/messages.go`: `recording_id` into `messageCols` (`:13`), `messageColsQualified` (`:16`), `scanMessage` (`:26-30`), and the insert column list and values (`:60-65`).
5. `internal/store/hydrate.go`: add `RecordingsForMessages` and call it from `hydrateRefs` (`:34-54`) beside attachments and mentions, so a paged timeline gets `duration_ms` and `size_bytes` without a second round trip.
6. `internal/app/publish.go`: add `PostRecordingNotice(ctx, containerID, authorID, recordingID uuid.UUID, body string)` delegating to `postSystem` with `RecordingID` set and no `CallID`, because `liveCallOf` (`message.js:198-202`) would otherwise put a Join button on the recording notice. Body text is plain, since `systemNode` renders `plainText(m.body)`: `"@alice recorded 4m 12s of the call"`, falling back to `"@alice recorded the call"` when the duration is unknown. It still reads correctly after retention nulls `recording_id`.
7. `internal/server/handlers/calls.go`, `finishEgress` (`:224`): return early unless `info.GetStatus() == livekit.EgressStatus_EGRESS_COMPLETE`, setting `store.RecordingFailed` through `setRecording` otherwise; look the row up with `RecordingByEgressID` and tolerate `ErrNotFound`; require exactly one `FileResults` entry with a non-zero size, and `os.Stat` the app-side `StoragePath` for a non-zero size before believing the file is usable; `FinishCallRecording`; `PostRecordingNotice`, swallowing `store.ErrConflict`; then `setRecording` to `store.RecordingReady`. Duration stays `int(f.GetDuration() / int64(time.Millisecond))`, because `FileInfo.Duration` is a nanosecond difference (`pkg/pipeline/controller.go:1010`). The room-ended stop needs no extra branch: `room_finished` already calls `stopEgress`, which produces the same `egress_ended`, so the notice posts through the identical path. It must keep setting `Processing` before `EndCall`.
8. Same file, new `Audio(w, r)`: `pathUUID(r, "id")`, then the recording, then `CallByID`, then `containerFor` (`internal/server/handlers/containers.go:349-362`) for the object-level check, then `OpenRecording`, then `Cache-Control`, then `serveFile(w, r, f, name, rec.Mime, queryBool(r, "download"))`.
9. `internal/server/router.go`: one route beside the attachment routes, `mux.Handle("GET /api/recordings/{id}", s.user(calls.Audio))`. One route with `?download=1` serves both playback and download, matching the existing `queryBool` helper (`handlers.go:243-246`) and `serveFile`'s `download bool` parameter (`media.go:199,211-216`), so the filename is decided in one place.

Range support is required and `serveFile` already provides it, ending in `http.ServeContent`, which parses the range header, replies `206`, and sets `Accept-Ranges: bytes`. A plain `io.Copy` would leave the browser unable to seek past what it had buffered. `http.ServeFile` would work too but takes a path and re-inspects `r.URL.Path`; `serveFile` takes the already-opened file and is the right reuse.

10. Docs. `docs/decisions.md:9` records "track egress, never room composite" and cites a CPU figure that belongs to the Chrome path (`room_composite_cpu_cost = 4`, `pkg/config/service.go:32-35`), not the SDK audio path; rewrite it to name the SDK audio path and keep the surviving half of the reasoning, that a mix cannot be diarised by track. `docs/deployment.md:116` is wrong about retention deleting recordings and becomes true only for the new flat layout. `README.md:77` describes per-speaker files.

### Frontend

`internal/server/static/css/input.css`: one rule under the pseudo-element exception, since range thumbs are `::-webkit-slider-thumb` and `::-moz-range-thumb`, which Tailwind does not reach: `.recording-seek { accent-color: var(--ctp-mauve); }`.

`internal/server/static/js/ui/audio.js`, new, exporting `recordingPlayer(rec)` built from `el` and `icon` (`ui/dom.js:8-21`):

```
div  mt-1 flex w-full max-w-md items-center gap-2 rounded-xl bg-base px-2 py-1.5
├── button  grid h-8 w-8 shrink-0 place-items-center rounded-full bg-mauve text-crust   play / pause
├── input[type=range]  recording-seek h-1 min-w-0 flex-1
├── span  shrink-0 text-micro tabular-nums text-overlay1        "0:00 / 4:12"
├── a[href="/api/recordings/{id}?download=1"]  shrink-0 text-overlay1 hover:text-text   download
└── audio  hidden, preload="metadata", src="/api/recordings/{id}"
```

`bg-base` is the correct well inside the `mantle` timeline. Total duration comes from `rec.duration_ms` so the label is right before metadata loads; `timeupdate` drives the range value and the elapsed half; `input` on the range sets `currentTime`. Play and pause swap the icon and re-run `drawIcons` on the button. Size it fluid: the message column's width now changes continuously during a drag, so a fixed-pixel width or a `min-width` above roughly 330px will overflow at the pane minimum.

A custom bar rather than `<audio controls>`. The page is themed light and dark from `--ctp-*` and a user-agent widget follows neither, Chrome's native control set adds a playback-rate and download menu the user did not ask for, and every other control in the timeline is already built from `el()` and `icon()`. This leaves the timeline carrying two audio idioms, since the audio attachment at `message.js:60-69` uses `<audio controls>`; do not convert that one in this change.

`internal/server/static/js/ui/message.js`: in `systemNode` (`:204-214`), append `recordingPlayer(m.recording)` after the text span when `m.recording` is present, and make the article `flex-col` so the player sits under the centred line. Add `m.recording ? m.recording.id : ''` to `messageSignature` (`:290-307`). It must be an identity and never playback state, because `nodeFor` replaces the node whenever the signature changes (`ui/timeline.js:118-129,163-170`), which would stop playback mid-track. Player state lives on the DOM node and in the `<audio>` element, never in `state`, so nothing calls `notify()`.

## Out of scope

- Per-thread read state. Threads carry none today, so "mark as read on open" governs channels and DMs.
- Converting the audio attachment player at `message.js:60-69` to the new bar.
- `GET /api/calls/{id}/recordings` stays and stays uncalled.
- Legacy per-call recording directories on the host, which remain undeletable until someone changes their ownership by hand.

## What a reviewer re-exercises

Hidden tab with push on and with push off; hidden tab receiving into the container that is open; the double-banner suppression while visible. A `?m=` deep link with unread present; a container switch during the marker linger; the New pill in both modes. Typing then sending in a channel, in a DM, and in a thread, and typing continuously past five seconds to confirm no flicker. Two participants where one leaves and the other stays, then a single participant whose network blips. A reconnect during a live call, confirming the recording badge and the Leave button still track. Start, stop, and restart recording twice in one call, confirming two notices; end a call while recording and confirm the notice still posts; join a call mid-recording and confirm the newcomer is in the mix. Play, pause, seek, and download from the timeline, and confirm the recording notice carries no Join button while the call is still live. The sidebar drag, its arrow keys, and its collapse. Both panes docked at 1280px and 1536px, and the mobile layout at 390px and 768px, where both handles stay invisible and untabbable. A retention sweep, confirming file and row both disappear.
