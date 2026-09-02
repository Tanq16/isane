# Implementation plan

Isane cannot currently display a message. Beyond that, the interface has no layering, no icons, two of twenty-six palette colours, a border on almost every element, and no light theme. This plan covers the repairs, the redesign, the feature work the redesign depends on, and the security fixes. Calls, voice, video, screen share, and recording are out of scope.

Work splits into a backend stream and a frontend stream. The backend stream lands first where the frontend depends on it; the dependency is named on each item.

## 1. Decisions

These were contested and are settled. Do not re-open them while implementing.

| Decision | Ruling |
|---|---|
| Message column layering | The reading surface steps up to `mantle` and the chrome stays on `crust`. Today every region paints `crust` and a 1px hairline is the only separator, which is what makes the app read as flat and over-ruled. |
| Latte re-slot | Section 2.7. `mantle` stays the reading surface and stays lighter than `crust`, matching the dark direction. Accents are darkened once, globally, until each clears 4.5:1. |
| Theme selector | `<html class="dark">` with Latte under `:root:not(.dark)`. Storage key `isane-theme`, holding `dark` or `light`. First visit with nothing stored follows `prefers-color-scheme`. |
| Admin navigation | A horizontal pill rail, not a vertical nav. Four sections: Identities, Channels, Invites, Stats. |
| Sidebar | Collapsible and resizable by drag, width persisted in `localStorage`. |
| Server setting storage | A new `server_settings` table with a typed column, not a `server_meta` key. `server_meta` is untyped internal bookkeeping and holds one string. |
| Channel creation default | `allow_member_channels` defaults to `true`. Anyone creates a channel unless an admin turns it off. |
| Right pane breakpoint | `xl`, not `md`. At 768px a `w-72` sidebar plus a `md:w-96` pane leaves the message column 96px. |
| Channel unread treatment | A dot for an unread channel and a red count pill only for mentions. A conversation keeps its count, because a DM count is the message count. |
| Attach icon | `paperclip`. |

Two calls are lower confidence and are flagged rather than hidden. The first is one flag governing both channel creation and channel editing rather than two; splitting them later is a one-column migration. The second is following `prefers-color-scheme` on a first visit rather than defaulting to dark.

## 2. The design system

Every class named here compiles under the pinned `tailwindcss v4.3.3` at `dist/tailwindcss`. Every Lucide name is a top-level key in the vendored `internal/server/static/vendor/lucide.min.js` at v1.38.0; verify a name by grepping `[{,]<PascalName>:` before using it.

### 2.1 Region tokens

| Region | Token |
|---|---|
| Page ground | `crust` |
| Sidebar panel and footer | `crust` |
| Call pane | `crust` |
| Admin page ground and pill rail row | `crust` |
| Message column: header, timeline, composer rail | `mantle` |
| Thread pane | `mantle` |
| Admin content surface | `mantle` |
| Modal panel | `mantle` plus `shadow-pop` |
| Popover, dropdown, mention autocomplete, hover action bar, toast | `base` plus `shadow-pop` |
| Code block inside a message | `base` |
| Text input, search field, composer field, chip on a surface, admin stat tile | `surface0` |
| Channel row hover | `surface0/60` |
| Message row hover, admin table row hover | `surface0/40` |
| Channel row active | `surface0` plus a `mauve` left rule |
| Composer reply banner, chip inside a control | `surface1` |
| Modal scrim | `scrim` plus `backdrop-blur-sm` |
| Message row that mentions you | `red/8` with `border-l-2 border-red` |
| Mention count pill | `bg-red text-crust` |
| Unread rail on a sidebar row | `bg-text` |
| Presence dot, online | `bg-green` |

Text runs one ramp: `text` for an author name, a heading, an active or unread channel name, and the one value a row exists to show; `subtext0` for message body, a channel name at rest, and form labels; `overlay1` for a timestamp, a topic, a handle, a section header, a table header, and every placeholder. `overlay0` is for a disabled label and the hover-only gutter timestamp, and never for a placeholder, because `overlay0` on `surface0` is 2.57:1.

### 2.2 Hue roles

One hue per meaning. A hue with no role does not appear.

| Hue | Role |
|---|---|
| `mauve` | Brand, focus ring, the active sidebar row rule, and the single primary action in a view |
| `blue` | Navigation targets: markdown links, a human `@handle`, the thread reply count, jump to message |
| `lavender` | Agents: the agent name, the `AGENT` badge, an agent `@handle`, and every section label |
| `red` | Addressed to you, and destruction: the mention pill, a message that mentions you, the new-messages divider, delete, deactivate, archive confirm, failed send |
| `green` | Presence and completion: the online dot, upload complete, socket live, refresh |
| `yellow` | In flight: sending, uploading, reconnecting, an invite inside its last day |
| `peach` | One-time and irreversible: the invite URL block, the agent claim token block, the archived badge, retention warnings |
| `teal` | A live call |
| `pink`, `flamingo`, `rosewater`, `maroon`, `sky`, `sapphire` | Avatar fallback fills only |

Accent is forbidden on a region background, on any border or divider, on body text, on an icon at rest, and on a hover state. The single exception is a message that mentions you. An icon is `overlay1` at rest and goes to `text` on hover, or to its own action hue where the action repeats in a row of icons.

Avatar fallbacks are deterministic, which colours the message list without touching any chrome:

```
HUES = [blue, mauve, green, peach, pink, teal, lavender, maroon, sky, flamingo, yellow, sapphire]
hue(user) = HUES[ sum(charCodeAt over user.id) mod 12 ]
```

The letter avatar becomes `bg-<hue> text-crust font-semibold`, replacing `bg-surface1 text-subtext1`. `text-crust` is correct in both themes, because `crust` is darkest in Mocha and lightest in Latte.

### 2.3 The border test

Apply to every edge in order. The first rule that matches wins.

1. Do the two surfaces carry different background tokens? No border. The step is the separator.
2. Do they carry the same token, and does one scroll under the other? `border-b border-surface0` or `border-l border-surface0`, one edge.
3. Do they carry the same token, and is one a list of rows the eye must count? `divide-y divide-surface0/50` on the container, never a border per row.
4. Otherwise nothing. Use whitespace.

The tree carries 145 border utilities today. The ones that survive are the channel header bottom edge, the message column to thread pane edge, the admin table header rule, the mention autocomplete popover, and `ring-1 ring-edge` on an image, a video, an avatar, and a modal. `--ctp-edge` is `transparent` in Mocha and a real hairline in Latte, so those elements carry the class unconditionally and need no theme variant.

A timeline draws exactly two horizontal rules: the date separator and the new-messages divider. Both are a rule with a label, not a plain border.

### 2.4 Scale

Spacing uses `0.5 1 1.5 2 2.5 3 4 6 8 12` and nothing else.

| Where | Class |
|---|---|
| Sidebar outer padding | `px-2 py-2` |
| Gap between sidebar sections | `mt-4` |
| Channel row | `h-8 px-2 gap-1.5` |
| Message column horizontal padding | `px-4` |
| Message row vertical padding | `py-0.5`, and `mt-4` before a new group |
| Avatar to message text | `gap-4` |
| Header height, thread header, admin header | `h-12` |
| Composer rail | `px-4 pb-6` |
| Modal | `p-4` with an `h-12` title row |
| Icon button | `h-8 w-8` in chrome, `h-7 w-7` in the hover action bar |
| Empty state owning a pane | `py-16` |

Radius: `rounded-md` for a row, a chip, or an icon button; `rounded-lg` for a button, an input, or a popover; `rounded-xl` for the composer field, a card, an attachment preview, or a code block; `rounded-2xl` for a modal; `rounded-full` for an avatar, a count pill, a presence dot, an unread rail, a badge, and every admin nav pill. Message rows have no radius, because full-bleed hover is what makes the timeline read as one surface.

Typography adds two tokens to `@theme`:

```css
--text-message: 0.9375rem;
--text-message--line-height: 1.375rem;
--text-micro: 0.6875rem;
--text-micro--line-height: 1rem;
--color-scrim: var(--ctp-scrim);
--color-edge: var(--ctp-edge);
--shadow-pop: var(--ctp-shadow-pop);
```

and to the Mocha `:root` block:

```css
--ctp-scrim: rgb(17 17 27 / 0.7);
--ctp-edge: transparent;
--ctp-shadow-pop: 0 8px 24px rgb(0 0 0 / 0.45);
```

| Role | Class |
|---|---|
| Wordmark | `font-display text-lg font-bold text-text` |
| Admin page title | `font-display text-xl font-semibold text-text` |
| Message author, channel header name, modal title | `text-message font-semibold text-text` |
| Message body, channel row, picker row | `text-message text-subtext0` |
| Admin nav pill, table cell, form label | `text-sm` |
| Timestamp, topic, handle, thread summary, typing indicator | `text-xs text-overlay1` |
| Section label | `text-micro font-bold uppercase tracking-widest text-lavender` |
| Gutter timestamp, count badge | `text-micro tabular-nums` |
| Code | `font-mono text-[0.8125rem]` |

`font-display` is confined to the wordmark and page-level titles. It is currently applied to a `text-sm` channel title, where Google Sans at 14px buys nothing.

### 2.5 Shell

```
< md              md .. xl                       xl and up
[ messages ]      [ sidebar | messages ]         [ sidebar | messages | pane 380 ]
  sidebar drawer    pane is a full-screen sheet    pane opens inline
```

Sidebar defaults to 240px, is resizable by a drag handle between 200px and 400px, and collapses to zero on `md` and up. The width lives in one CSS custom property so the handle writes one value, and persists under `isane-sidebar-width`. The collapse control is `panel-left-close` in the channel header, becoming `panel-left-open` when collapsed. The drawer on small viewports keeps one implementation: the shell markup in `index.html` owns positioning and `sidebar.js` owns content.

The right pane holds exactly one of thread, call, or members. Opening one closes the others. The message column takes no max width.

### 2.6 Message list

A message joins the previous group when every condition holds, and otherwise starts a new group:

```
same author_id
AND created_at - previous.created_at <= 7 minutes
AND same calendar day
AND this message has no reply_to_id
AND neither message is the first after a date separator or the new-messages divider
```

A group head renders a 40px avatar, the author name, an `AGENT` badge for an agent, and a timestamp reading `Today at 14:32`, `Yesterday at 14:32`, or `01/09/2026 14:32`. A continuation renders a hover-only right-aligned `14:33` in the 40px gutter and no avatar. Text starts at 72px from the column edge in both cases.

A message with `is_system: true` renders as one centred `overlay1` line with no avatar, no author name, no badge, and no hover actions. It is a status line, not a message anyone can reply to.

### 2.7 Light theme

```css
:root:not(.dark) {
  --ctp-crust:    #dce0e8;
  --ctp-base:     #e6e9ef;
  --ctp-mantle:   #eff1f5;
  --ctp-surface0: #ccd0da;
  --ctp-surface1: #bcc0cc;
  --ctp-surface2: #acb0be;

  --ctp-text:     #4c4f69;
  --ctp-subtext1: #5c5f77;
  --ctp-subtext0: #5c5f77;
  --ctp-overlay2: #6c6f85;
  --ctp-overlay1: #6c6f85;
  --ctp-overlay0: #8c8fa1;

  --ctp-mauve:    #7e35de;
  --ctp-red:      #c30e35;
  --ctp-blue:     #1a59d5;
  --ctp-teal:     #116c71;
  --ctp-green:    #2d701e;
  --ctp-lavender: #4e5cac;
  --ctp-yellow:   #955f13;
  --ctp-peach:    #b94908;
  --ctp-pink:     #9f508a;
  --ctp-maroon:   #c13a46;
  --ctp-sky:      #0373a0;
  --ctp-sapphire: #187788;
  --ctp-rosewater: #b4795f;
  --ctp-flamingo:  #b45f5f;

  --ctp-scrim:      rgb(76 79 105 / 0.45);
  --ctp-edge:       #bcc0cc;
  --ctp-shadow-pop: 0 8px 24px rgb(76 79 105 / 0.18);
}
```

Stock Latte accents on `#eff1f5` measure blue 4.34, lavender 2.81, teal 3.31, green 2.96, yellow 2.31, peach 2.64, and all fail as text. Stock Latte `mauve` under `text-crust` measures 4.09 and fails as a filled button. The values above clear 4.5:1 as text and 4.5:1 under `text-crust` as a fill. Verify each ratio before shipping and correct any that misses.

Seven things change beyond token values:

1. Elevation flips from lightness to shadow. A modal cannot go lighter than a near-white reading surface, so `shadow-pop` carries it.
2. `ring-1 ring-edge` becomes visible on images, videos, avatars, and modals.
3. Hover flips direction on its own, because `surface0` is lighter than `mantle` in Mocha and darker in Latte.
4. The syntax highlighting theme has to follow. Either vendor a second `highlight.js` stylesheet and flip the `disabled` flag on two `<link>` elements, or delete the vendored stylesheet and write the `.hljs-*` rules against `--ctp-*`. Prefer the second, which is the smaller surface and matches how `css/input.css` already themes markdown.
5. Mermaid must be re-initialised and every mounted diagram re-rendered on switch, because it inlines computed colours into the SVG.
6. `<meta name="theme-color">` and `<meta name="color-scheme">` must switch with the theme.
7. A head script must set the class before the body paints, or the page flashes Mocha on every light-mode load.

`manifest.json` colours are read at install time and cannot switch. Leave them on the dark values.

The toggle is a `sun` and `moon` icon button in the sidebar footer beside sign out.

### 2.8 Icons

Lucide everywhere. `lucide.createIcons()` runs after every DOM insertion that adds an `<i data-lucide>`, scoped with `{ nodes }` the way `js/render.js` already does. Icon sizing is `h-5 w-5` in chrome, `h-4 w-4` in dense rows and the hover action bar, `h-3.5 w-3.5` inside a chip, and never scaled with `text-*`.

| Affordance | Icon |
|---|---|
| Channel | `hash` |
| Direct messages, members | `users` |
| New conversation | `square-pen` |
| Search | `search` |
| Collapse, expand a section | `chevron-down`, `chevron-right` |
| Attach a file | `paperclip` |
| Markdown preview | `eye` |
| Send | `send-horizontal` |
| Reply inline | `corner-up-left` |
| Reply quote marker | `corner-up-right` |
| Reply in thread, thread pane title | `message-square-text` |
| Edit | `pen-line` |
| Delete | `trash-2` |
| More actions | `ellipsis` |
| Close, remove a chip, dismiss | `x` |
| Confirm, current selection | `check` |
| Notification level: all, mentions, none | `bell`, `at-sign`, `bell-off` |
| Start a call, call in progress, leave | `phone`, `phone-call`, `phone-off` |
| Sidebar collapse, expand | `panel-left-close`, `panel-left-open` |
| Mobile drawer | `menu` |
| Back | `arrow-left` |
| Channel settings | `settings` |
| Create a channel | `plus` |
| Sign out | `log-out` |
| Theme toggle | `sun`, `moon` |
| Admin entry, grant or revoke admin | `shield` |
| Admin sections: Identities, Channels, Invites, Stats | `users`, `hash`, `ticket`, `chart-column` |
| Agent badge | `bot` |
| Reset a password | `key-round` |
| Deactivate a person | `user-round-x` |
| Archive, unarchive | `archive`, `archive-restore` |
| Refresh | `refresh-cw` |
| Copy | `copy` |
| Download | `download` |
| In flight | `loader-circle` with `animate-spin` |
| Warning, error, info | `triangle-alert`, `circle-alert`, `info` |

### 2.9 Component shapes

Composer:

```html
<div class="shrink-0 px-4 pb-6">
  <div class="overflow-hidden rounded-xl bg-surface0 focus-within:ring-1 focus-within:ring-surface2">
    <!-- reply banner on bg-surface1, only when replyToId is set -->
    <!-- attachment chips, only when attachments exist -->
    <div class="flex items-end gap-1 px-2 py-1.5">
      <button class="grid h-9 w-9 shrink-0 place-items-center rounded-lg text-overlay1 transition-colors hover:text-text" title="Attach a file">
        <i data-lucide="paperclip" class="h-5 w-5"></i>
      </button>
      <textarea rows="1" placeholder="Message #general"
        class="max-h-80 min-h-9 flex-1 resize-none bg-transparent py-2 text-message text-text placeholder:text-overlay1 focus:outline-none"></textarea>
      <button ... title="Preview markdown"><i data-lucide="eye" class="h-5 w-5"></i></button>
      <button class="... text-mauve hover:bg-surface1 disabled:text-overlay0" title="Send. Shift and Enter adds a line">
        <i data-lucide="send-horizontal" class="h-5 w-5"></i>
      </button>
    </div>
  </div>
  <p class="h-5 px-2 pt-1 text-xs text-overlay1"><!-- typing indicator, height reserved --></p>
</div>
```

The field has no border. The placeholder is `Message #general` for a channel, `Message @tanq` for a one-to-one conversation, `Message Tanq, Ana` for a group, and `Reply in thread` in the thread pane. The sentence `Enter sends, Shift plus Enter adds a line` leaves the page and becomes the send button's `title`. Send stays visible and disabled when the field is empty. The dragover state is `ring-2 ring-mauve` on the field.

Channel header:

```html
<header class="flex h-12 shrink-0 items-center gap-2 border-b border-surface0 px-4">
  <!-- menu on mobile, panel-left-close on md and up -->
  <i data-lucide="hash" class="h-5 w-5 shrink-0 text-overlay1"></i>
  <h1 class="shrink-0 text-message font-semibold text-text">general</h1>
  <!-- archived badge, only when archived -->
  <span class="mx-1 h-6 w-px shrink-0 bg-surface0"></span>          <!-- only when a topic exists -->
  <p class="min-w-0 flex-1 truncate text-sm text-overlay1">the topic</p>
  <!-- right, in order: call, notification level, channel settings, search -->
</header>
```

One bar, full width, one background, one bottom border. The current header renders a second bar inside the `<header>` which sizes to its content and paints its own `bg-crust`, and that is the floating tab.

Hover action bar: `absolute -top-3 right-4` on `bg-base` with `shadow-pop`, no border, five `h-7 w-7` icon buttons. It must also be reachable by keyboard, so it carries `group-focus-within:flex` alongside `group-hover:flex` and the article is focusable. In v4.3.3 `group-hover:` compiles inside `@media (hover: hover)`, so below `md` render the `ellipsis` button persistently and open the same actions as a sheet.

Admin pill rail:

```html
<nav class="flex items-center gap-1 rounded-full bg-base p-1" aria-label="Administration">
  <button data-section="identities" aria-current="page"
    class="flex items-center gap-1.5 rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors">
    <i data-lucide="users" class="h-4 w-4"></i><span class="hidden sm:inline">Identities</span>
  </button>
  <!-- hash Channels, ticket Invites, chart-column Stats -->
</nav>
```

Active takes `bg-surface0 text-text` and `aria-current="page"`; inactive takes `text-subtext0`. The label collapses to icon-only under `sm`.

People picker, replacing the inline checkbox list: a centred modal on `bg-mantle` with `shadow-pop`, a chip field, an autofocused input, a scrollable row list showing avatar, display name, `@handle`, and a presence dot. Selection is the row itself, arrow keys move an `aria-selected` highlight, Enter toggles, Backspace on an empty field removes the last chip, and Escape closes. There is no Cancel button; Escape, the `x`, and a scrim click all close. The primary button reads `Message @bob` for one person and `Start group message` for more.

Every `window.confirm` and `window.prompt` in the tree moves into this same modal shell, which `#modal-root` already exists to hold and which nothing currently writes to. A destructive confirmation turns its title and its confirm button red and states the consequence above the footer.

Empty states are a centred `text-subtext0` line at `py-16`, with no icon, no card, and no border.

Toasts move to bottom-right, drop the border that matches their own fill, keep `shadow-pop`, and take a severity argument colouring the message text `blue`, `green`, `red`, or `yellow`.

Every interactive element carries `transition-colors`. Row hover backgrounds do not transition, because a transition on a row the pointer sweeps across leaves a trail of half-lit rows.

## 3. Backend workstream

Go, `internal/`. Items 3.1 through 3.4 unblock the frontend and land first.

### 3.1 Config ownership

`internal/app/app.go:53-76` copies `config.Config` by value into `media.New`, `push.New`, `calls.New`, `agents.New`, `retention.New`, and stores a sixth copy on `App.Cfg`. No setting can change without a restart. Replace the copies with a single `*config.Config` owned by `App` and read through an accessor. This is the prerequisite for every runtime setting.

### 3.2 Server settings

New migration `internal/store/migrations/0002_server_settings.sql`:

```sql
create table server_settings (
    id                    boolean primary key default true,
    allow_member_channels boolean not null default true,
    updated_at            timestamptz not null default now(),
    updated_by            uuid null references users(id) on delete set null,
    constraint server_settings_singleton check (id)
);

insert into server_settings (id) values (true);
```

`primary key` plus `check (id)` makes the singleton structural. `updated_by` is nullable so the seeded row needs no user, which is the treatment `invites.created_by` already has. `Migrate` in `internal/store/store.go:42-85` sorts `migrations/*.sql` by filename and needs no runner change.

New `internal/store/settings.go`:

```go
func (db *DB) ServerSettings(ctx context.Context) (ServerSettings, error)
func (db *DB) UpdateServerSettings(ctx context.Context, allowMemberChannels bool, actor uuid.UUID) (ServerSettings, error)
```

The update is one `update ... returning` statement, so no transaction and no cache. `internal/store/models.go` gains:

```go
type ServerSettings struct {
    AllowMemberChannels bool       `json:"allow_member_channels"`
    UpdatedAt           time.Time  `json:"updated_at"`
    UpdatedBy           *uuid.UUID `json:"updated_by,omitempty"`
}
```

One flag gates creating a channel and editing a channel's name or topic. Archiving stays admin only, because it hides content.

### 3.3 Channel routes move off the admin prefix

New `internal/server/handlers/settings.go` holding `Settings` with `Get` and `Update`. Channel create and update move into `internal/server/handlers/containers.go`, which already owns `containerFor`. `slugPattern` and `trimTopic` move with them. `Admin.CreateChannel` and `Admin.UpdateChannel` are deleted with their routes, because one action carrying two authorization rules is two rules to keep in step.

```go
mux.Handle("GET   /api/settings",        s.user(settings.Get))
mux.Handle("PUT   /api/settings",        s.adminOnly(settings.Update))
mux.Handle("POST  /api/channels",        s.user(containers.CreateChannel))
mux.Handle("PATCH /api/channels/{id}",   s.user(containers.UpdateChannel))
mux.Handle("GET   /api/containers/{id}", s.user(containers.Get))
```

`GET /api/admin/channels`, `DELETE /api/admin/channels/{id}`, and the unarchive route stay admin only.

Authorization:

```go
func (h *Containers) mayManageChannels(ctx context.Context, u store.User) (bool, error) {
    if u.IsAdmin {
        return true, nil
    }
    s, err := h.app.DB.ServerSettings(ctx)
    if err != nil {
        return false, err
    }
    return s.AllowMemberChannels, nil
}
```

Denied returns `forbiddenf("only an administrator creates a channel")` and `forbiddenf("only an administrator edits a channel")`.

Validation, all new: `slug` matches `^[a-z0-9][a-z0-9-]{0,63}$` after lowercasing and trimming; `name` is non-empty after trimming and at most 64 characters; `topic` is at most 256 characters and an empty trim stores NULL; `slug` is immutable and a `PATCH` carrying one is a 400. An empty `name` on a `PATCH` is a 400, not a silent keep. A duplicate slug is a 409. Apply the same 64-character cap to `display_name`. A 5000-character channel name is accepted today.

Create returns 201 with the `ContainerView` for the creating user, not the bare `Container`, so the client can insert it into the sidebar with `unread`, `mentions`, and `level` already set.

### 3.4 Websocket frames

`internal/socket/frame.go` gains, in the outbound block:

```go
TypeSettings   = "settings"
TypeContainer  = "container"
TypeUser       = "user"
TypeAttachment = "attachment"
```

`TypeAttachment` replaces the bare `frameAttachment` string literal in `internal/server/handlers/media.go:21`, which is why nobody noticed the client has no case for it.

`internal/socket/hub.go` gains `ToAll`, using the same copy-under-`RLock` then send-outside shape as `ToUsersExcept`.

| Trigger | Frame | Audience | Payload |
|---|---|---|---|
| `PUT /api/settings` | `settings` | `ToAll` | `store.ServerSettings` |
| Channel created, renamed, re-topiced, archived, unarchived | `container` | `ToAll` | `store.Container` |
| Conversation created | `container` | `ToUsers(participants)` | `store.Container` |
| User created, deactivated, admin flag changed, invite accepted | `user` | `ToAll` | directory projection |
| Agent reserved or deleted | `user` | `ToAll` | directory projection |

The payload is `store.Container`, not `store.ContainerView`. The view's `unread`, `mentions`, `level`, and `participants` are per-user, and broadcasting one user's view would clobber every other client's badges, because the client merges field by field. The notification preference is deliberately absent from the table, because broadcasting it would leak one user's preference to everyone.

### 3.5 Ready payload

`readyPayload` in `internal/app/handler.go:17-37` is unexported and returned as `any`, which is why the client reads a `d.users` field that has never existed. Export it as `ReadyPayload` and add:

- `users`, the full directory projection. This closes the reconnect window as well as the initial load.
- `presence`, the online user ids from the existing `Hub.OnlineUsers()`. Without it every user reads offline until they happen to reconnect.
- `calls`, the live calls from the existing `DB.ListLiveCalls`. Without it a user who reloads during a call sees no join prompt.
- `settings`, the `ServerSettings` row, so the client can decide whether to draw the create-channel control on first paint.

Pass `includeArchived: true` to `ContainerViews`. The sidebar already builds an `Archived` group that no data can currently reach, and the admin channel table cannot see an archived channel at all.

### 3.6 Correctness fixes

| Item | Location | Fix |
|---|---|---|
| An unread badge from a thread reply can never clear | `internal/store/containers.go:34`, `internal/store/readstate.go:36` | Replace the `last_seq - last_read_seq` arithmetic with `(select count(*) from messages m where m.container_id = v.id and m.thread_root_id is null and m.seq > coalesce(r.last_read_seq, 0) and m.deleted_at is null)`. This also stops soft-deleted messages counting. The index it needs already exists and is already partial on `thread_root_id is null`. |
| A message can take a thread reply as its thread root | `internal/app/publish.go:87` | Load the thread root and reject with `ErrInvalid` when its `ThreadRootID` is non-nil. Do not silently re-anchor. The resulting message is reachable from no view a client constructs and it does raise the unread count. |
| Three domain operations exist twice and disagree | `internal/app/publish.go:131-184` against `internal/server/handlers/messages.go:154-230` and `internal/server/handlers/containers.go:52-78` | The REST handlers call `app.editMessage`, `app.deleteMessage`, and `app.markRead`. Delete `editedFrame`, `deletedFrame`, `readFrame`, and `Messages.broadcast`. The socket path currently skips the archived-channel check and the mark-read membership check, and the socket path is the one the client uses. |
| Three copies of the same broadcast helper | `internal/app/publish.go:35`, `internal/server/handlers/calls.go:337`, `internal/server/handlers/messages.go:358` | Export `(*App).Broadcast` and delete the two in `handlers`. |
| A queued agent job never expires and wedges the container | `internal/store/agents.go:251` | Widen `ExpireStaleJobs` to `(state = 'dispatched' and dispatched_at < $1) or (state = 'queued' and created_at < $1)`. |
| An untracked goroutine runs attachment processing | `internal/server/handlers/media.go:61` | Route through the `App.spawn` and `App.guard` pair every other background job uses. A panic in `media.Process` currently takes down the process. |
| Environment overrides are unnamespaced | `internal/config/config.go:186` | Prefix the computed name with `ISANE_`. A stray `DATABASE_URL` silently beats `config.yaml` today. Update the table in `docs/deployment.md`. |
| Two invites redeemed concurrently against an empty database both mint an admin | `internal/server/handlers/auth.go:131` | Move `CountUsers` inside the `AcceptInvite` transaction. |
| A deleted message leaves its thread's reply count overstated | `internal/store/messages.go:157` | Decrement the thread root's `thread_reply_count`. |
| `TimelineAround` gives the anchor to the lower half | `internal/store/messages.go:190` | An even limit returns one fewer message of context before the anchor than after. |
| Raw wrapped Go errors reach the user | `internal/server/handlers/handlers.go:97` | A user reading `accept invite: invite already used: conflict` sees three fragments and a sentinel where one sentence belongs. Return the message without the wrapping chain. |
| `handlers.go` maps only two SQLSTATEs | `internal/server/handlers/handlers.go:106` | A check-constraint (23514), foreign key (23503), or invalid enum input (22P02) becomes a 500. |
| No preflight for the media toolchain | `internal/media/` | `exec.LookPath` for `magick`, `ffmpeg`, and `ffprobe` at startup. A missing binary is a warning naming what stops working, not a fatal. Surface the result on `GET /api/admin/stats`. |
| A failed attachment is unrecoverable | `internal/server/handlers/media.go:113` | Serve a `failed` attachment's original bytes with `Content-Disposition: attachment` and its original mime. The bytes are intact on disk. `/thumb` keeps returning 404. |
| `ListOrphanedAttachments` can never return a row | `internal/store/attachments.go:81` | `attachments.message_id` is `on delete set null`, so a hard-deleted message leaves `message_id is null`, which the query excludes. It runs on every sweep. |
| `NewFrame` swallows its marshal error | `internal/socket/frame.go:53` | A broadcast can silently deliver `{"t":"message"}` with no payload and no log line. |
| `sort.Strings` where the idiom is `slices.Sort` | `internal/store/store.go:58` | The only idiom deviation in the module. |

### 3.7 Security fixes

| Severity | Item | Location | Fix |
|---|---|---|---|
| High | Attachment download has no authorization check at all | `internal/server/handlers/media.go:99` | `ready()` checks only that a session exists. Any signed-in account reads any attachment in the deployment given its id, including one posted inside a conversation it is not part of. Resolve the owning message and require container membership through `containerFor`; when `message_id` is null require `uploader_id == caller`. `Get` and `Thumb` both route through `ready`, so one change covers both. |
| High | A password change or reset leaves every session valid | `internal/server/handlers/auth.go:177`, `internal/server/handlers/admin.go:172` | Call the existing `DeleteSessionsForUser` in both, then mint a fresh session for the actor in `ChangePassword` so the user stays signed in on the device they used. A stolen cookie currently survives the reset that was meant to close it. |
| Medium | CSRF rests only on `SameSite=Lax` | `internal/auth/cookie.go:22`, `internal/server/handlers/handlers.go:112` | Lax is site-scoped, so a sibling subdomain or another port on the same host is same-site and forges every state-changing request. Require `Origin` to equal `server.public_url` on every non-GET API request, rejecting an absent or different header, and require `Content-Type: application/json` in `ReadJSON`. |
| Medium | No rate limiting or lockout anywhere | `internal/server/handlers/auth.go:50`, `:99`, `internal/server/handlers/media.go:31` | 90 login attempts per second are sustained today, each driving an Argon2id computation at 64 MiB. Add a per-IP and per-account limiter with exponential backoff on `Login` and `AcceptInvite`, and a per-user concurrent-upload cap. |
| Medium | Login timing discloses whether an account exists | `internal/server/handlers/auth.go:61` | 28.21ms against 0.43ms. Verify the supplied password against a fixed dummy Argon2id hash when the lookup misses. |
| Medium | No security headers on the app page or the API | `internal/server/static.go:41`, `internal/server/server.go:37` | Add a middleware setting `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'none'; object-src 'none'`, plus `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `Strict-Transport-Security` when `server.insecure` is false. Without `Referrer-Policy` the invite path leaks its token in the `Referer` of any outbound navigation. |
| Medium | Invite tokens are written to the access log in cleartext | `internal/server/middleware.go:81`, `cmd/serve.go:101` | Redact the token segment, rewriting any `/invite/<rest>` path to `/invite/[redacted]`. Write the bootstrap invite to a file with mode `0600` or to stdout only, and log that it was written rather than its value. |
| Medium | A staged upload can be claimed by any other user | `internal/store/messages.go:85` | The update filters on `message_id is null` only. Add `and uploader_id = $3` and pass the message author. |
| Medium | `GET /api/users` returns every email to every signed-in user | `internal/server/handlers/containers.go:172` | Add a directory projection carrying `id`, `kind`, `handle`, `display_name`, `avatar_id`, `is_admin`, and `deactivated_at`. Use it for `GET /api/users` and for the `users` array and `user` frame. `GET /api/admin/users` keeps the full shape. |
| Medium | Push subscription ownership is keyed on the endpoint alone | `internal/store/push.go:26` | The upsert moves a row to the caller and replaces the encryption keys. On conflict, keep the row only when `user_id` is unchanged. Untested locally, because push is disabled without VAPID keys. |
| Low | Media processing errors leak absolute server paths to channel members | `internal/media/service.go:117` | Log the tool output server-side and store a fixed user-facing reason on the row. |
| Low | Mermaid output is never re-scrubbed and remote images load | `internal/server/static/js/render.js:223`, `:334` | Re-run `scrub` over each Mermaid container after `mermaid.run` resolves. Remote image loading is closed by the CSP above. |
| Low | Sessions are not rotated when the admin flag changes | `internal/server/handlers/admin.go:179` | Call `DeleteSessionsForUser` for the target in `SetAdmin`. The demotion direction is the one that matters. |
| Low | Session lifetime is effectively unbounded | `internal/auth/cookie.go:10` | 90 days, slid forward on every hour of use, with no absolute cap. Add an absolute lifetime independent of `last_seen_at`. |
| Low | Invite acceptance distinguishes used, expired, and unknown | `internal/store/invites.go:68` | Return one status and one message for all three. |
| Low | `server.insecure` silently drops the Secure flag | `internal/server/server.go:73` | Keep the flag and emit a startup warning naming what it disables. |

`govulncheck` reports no vulnerability reachable from this code. `cel-go` v0.29.0 carries `GO-2026-6094`, is reached only through the LiveKit protocol tree, and is worth bumping to v0.30.0.

### 3.8 First run

`cmd/serve.go` mints the bootstrap invite when no accounts exist, and a first administrator then lands in an app with no channels, no call to action, and no way to create one outside `/admin`. Create `#general` with name `General` and topic `Everything that does not have a home yet` inside `AcceptInvite` for the first user, because `containers.created_by` is `not null` and the user id does not exist at boot.

### 3.9 New endpoints for the settings surfaces

| Method | Path | Middleware | Body | Success |
|---|---|---|---|---|
| `PATCH` | `/api/auth/me` | `s.user` | `{"display_name","avatar_id"}`, both optional | 200 user, plus a `user` frame |

`display_name` is trimmed, non-empty, and at most 64 characters. `avatar_id` must name a ready image attachment uploaded by the caller, and `null` clears it. `users.avatar_id` already exists with a foreign key to `attachments`, and both the sidebar and the timeline already render it, so the endpoint is the only missing piece.

### 3.10 Build

`Makefile:88` declares `app.css` as depending on `input.css` and the Tailwind binary only, while `input.css:3-4` declares `@source "../index.html"` and `@source "../js"`. A new utility class in a JS file is therefore silently missing from a local `make build`. CI and Docker are unaffected, because both compile from a clean tree, which is why this has stayed hidden. It fires on the first commit of this work.

```make
$(CSS_DIR)/app.css: $(CSS_DIR)/input.css $(TAILWIND_BIN) $(STATIC_DIR)/index.html $(shell find $(STATIC_DIR)/js -name '*.js')
```

Add the no-op `self.addEventListener('fetch', () => {})` to `internal/server/static/sw.js`, which caches nothing and is what makes the app installable.

## 4. Frontend workstream

Vanilla ES modules under `internal/server/static/js/`. Items 4.1 and 4.2 are the repairs; everything after them is the redesign.

### 4.1 Make messages render

`upsertMessage` in `js/store.js:84` returns without storing when `state.messages` has no entry for the container, and nothing else creates that entry. Every paging call site feeds its page through it, so every page is discarded. The API returns the messages; the client throws them away. `js/ui/timeline.js:540` is the only path that seeds `state.messages` directly, which is why the `?m=<seq>` deep link is the sole working route into a timeline.

Route the paging sites through the `merge` path that `store.loadMessages` and `store.loadThread` already implement correctly, and keep the early return in `upsertMessage` itself, so a live socket frame does not create a half-populated list for a container the user has never opened.

| Site | What it loses today |
|---|---|
| `js/ui/timeline.js:563` | the first page of a channel |
| `js/ui/timeline.js:587` | `loadOlder` back-pagination |
| `js/ui/timeline.js:611` | `loadNewer` catch-up |
| `js/ui/thread.js:306` | the thread root |
| `js/ui/thread.js:308` | every thread reply |
| `js/store.js:134` | the thread root inside `loadThread` |
| `js/ui/composer.js:310` | the message saved over HTTP when the socket is down |
| `js/ui/socket.js:266` | every live message, because the list is still absent |

`js/ui/socket.js:248` also skips gap backfill for any container not in `state.messages`, so reconnect recovery is dead for the same reason.

### 4.2 The other repairs

| Item | Location | Fix |
|---|---|---|
| The message input is 0px tall until the user types | `js/ui/composer.js:339` | `autosize()` runs while the box is still `hidden`, so `scrollHeight` is 0 and the element is pinned at `height: 0px`. Call it after `renderNotice()` un-hides the box, and make it a no-op when `offsetParent` is null. |
| One long channel name makes the whole admin page unreachable | `js/ui/admin.js:118`, `:587` | Grid items default to `min-width: auto`, so the widest cell stretches every card to 42,632px and `scrollLeft` maxes at 0. Add `min-w-0` to the grid items and the table wrapper, and `truncate` on the cells. |
| Every admin agent row reads `@undefined` | `js/ui/admin.js:458-483` | The endpoint returns `[{user, agent}]` and the loop reads flat fields. Delete calls `/api/admin/agents/undefined`. |
| Creating an invite always returns 400 | `js/ui/admin.js:263` | `expires_in` is sent as `"168h"` against an `int64` of seconds. The select values become `86400`, `604800`, `2592000`. Invites are the only path to an account that can sign in. |
| Every search result renders blank | `js/ui/sidebar.js:258` | The endpoint returns `[{message, rank}]` and the loop reads bare messages, so every row shows `Unknown` and navigates to `?m=undefined`. |
| Socket `error` frames are parsed and discarded | `js/socket.js:224` | Nothing registers `socket.on('error')`, so every rejected send, edit, delete, read, and thread subscription is silent, and the optimistic message is replayed on every reconnect for the life of the tab. Match `client_id` to a pending message, mark it failed, and toast the server message. Map `code === "internal"` to `Something went wrong. Try again.` |
| The `attachment` frame is discarded | `js/socket.js:224` | An attachment on a posted message stays rendered as `processing` for the life of the page. The frame carries a bare attachment, so the handler locates the message by `message_id` and splices it in. |
| A stale close event kills the live socket | `js/socket.js:118` | `onClose` nulls `ws` unconditionally, so a closing socket's event nulls the reference to a newer open one. Capture the socket in the handler closure and ignore an event from a socket that is no longer current. |
| A second thread opened during a load never loads | `js/ui/thread.js:299` | `loading` is one module-level flag. Key the in-flight guard by `rootId`. |
| A 401 mid-session is invisible | `js/api.js:54` | No consumer branches on `err.status`, so a deactivated user or an expired session sits on a permanent Reconnecting toast against a server that will never accept them. Branch on 401 and route to `/login`. Wire `ws.onerror` and stop retrying a rejected upgrade. |
| `loadNewer` writes the wrong flag on failure | `js/ui/timeline.js:613` | The catch sets `p.loaded`, which is already true and is not the flag that controls retrying. |
| Every unknown call participant is labelled `Unknown` | `js/ui/call.js:200` | `store.user()` always returns a truthy placeholder, so the `participant.name` fallback is dead. Check `state.users.has(identity)`. |
| Focus is destroyed on every store notification | `js/ui/sidebar.js:363-365`, `js/ui/admin.js:559` | `replaceChildren` on every notify throws a keyboard user out of the sidebar whenever any message arrives anywhere. Reconcile rows by id the way the timeline already does. |
| The socket and the first render wait up to 3s on push init | `js/main.js:386` | Call `connect()` and `route()` first, then init push in the background. |
| The push banner covers the entire header | `index.html:55` | It is `fixed top-0 z-40` and its bottom edge lands at 57px over a header occupying 0 to 56px. Make it a flow element above the shell, or a toast. |
| `beforeunload` listeners disqualify the back/forward cache | `js/ui/composer.js:472`, `js/ui/thread.js:451` | |
| A typing interval and a `pointermove` handler run for the life of the page | `js/ui/composer.js:495`, `js/ui/call.js:511` | Both run whether or not anyone is typing and whether or not a call is active. |
| Message text renders at the wrong size | `css/input.css:149` with `js/ui/timeline.js:354` | `renderInto` appends a `.markdown-body` inside a host that already carries the class, so the nested `font-size: 16px` and `color: !important` beat the host's utilities. Put the class on exactly one element. |
| The reply quote cannot resolve a parent outside the loaded window | `js/ui/timeline.js:178` | It falls back to `m.reply_to`, a field the API never sends. Fetch the parent on demand, or hydrate a compact parent reference server-side. |
| The admin stats grid renders `[object Object]` | `js/ui/admin.js:503` | `Object.entries` flattens one level. |
| The copy button never reverts | `js/ui/admin.js:108` | Use the icon flip already implemented in `js/render.js:290-313`. |
| One `localStorage` key has two owners | `js/main.js:12`, `js/ui/sidebar.js:5` | Both define and write `isane:last-container`. |
| A successful login does a full document reload | `js/main.js:270` | |
| `boot()` has no catch | `js/main.js:369` | A non-`ApiError` from `/auth/me` leaves a permanently blank page with nothing shown. |
| `decode` returns raw text when the body is not JSON | `js/api.js:53` | An HTML error page becomes the user-visible error message. |

### 4.3 Shell and layering

Delete one of the two drawer implementations. `index.html:31-34` declares a `data-drawer` group with a `#drawer-scrim` that no script ever touches, while `js/ui/sidebar.js:370-388` re-applies its own positioning classes on top and builds a second backdrop, so the element carries both `z-50` and `z-40`. The same double shell exists for the thread pane and the call pane, where each `<section>` carries panel classes and each module nests a panel repeating all of them. The markup owns the shell; the module fills it.

Ground `#main` and its children consistently on `mantle` and stop repainting the ground below it. Give the header bar `w-full min-w-0`, drop its duplicated `bg-crust` and `border-b`, and let the controls collapse into an overflow menu below `md`. At 390px the header bar is currently 522px wide against a 390px viewport, so the notification control and the call button are off-screen and unreachable on a phone.

Remove the dead horizontal scrollbar: `overflow-y-auto` on `#sidebar` makes `overflow-x` compute to `auto`, and the inner `w-72` panel with its own `border-r` is 288px inside a 287px content box.

Give `main.js` one route table that owns `#admin-root` visibility and matches both `/admin` and `/admin/{section}`. Today `js/main.js:185` matches `/admin` exactly and bounces `/admin/identities` to `/`, while `js/ui/admin.js:30` accepts the prefix, and both files toggle the same element.

### 4.4 Timeline

Thread the group boundary through the render path. `signatureOf` at `js/ui/timeline.js:307` covers only a message's own fields, so a node whose rendering depends on its predecessor cannot be invalidated when a neighbour changes. Compute the boundary in `visibleMessages()`, which already returns an ordered array, hang it on the message view object, and include it in the signature. The existing reconciler then handles it unchanged.

Add the date separator, the new-messages divider, the grouped and continuation rows, the icon hover action bar, the `is_system` status line, and the agent working indicator. `agent_working` and `agent_done` already reach `state.jobs` and nothing renders them, so mentioning an agent produces no feedback for up to the five-minute job timeout. Render a row pinned above the composer for every job whose `container_id` matches the open container, and let it disappear on `agent_done`, which the server sends on success, failure, and timeout.

Skip the unread increment on an incoming message carrying `thread_root_id`. A thread reply raises the thread's unread, not the channel's.

Move the jump highlight from `ring-2 ring-mauve`, which draws a box around a row that has no box, to a `bg-yellow/10` hold released after two seconds.

### 4.5 Channel settings and topics

A modal opened from the header, replacing the native `<select>` that currently sits between the channel title and the call button.

1. **About.** Name and topic as editable fields when the user may edit and read-only text otherwise, plus a read-only `#slug`, the creator, and the creation date. Save writes `PATCH /api/channels/{id}` and is disabled until something changes.
2. **Notifications.** Three radio options, not a select: `All messages`, `Mentions only` as the default, and `Nothing`, each with a one-line description. Writes `PUT /api/containers/{id}/notification-pref` on change and reverts inline on failure. Channels only, matching the server.
3. **Archive.** Admin only, and absent for everyone else. Archive or unarchive, with a second click on the same button as the confirmation. `POST /api/admin/channels/{id}/unarchive` has no caller anywhere today, so archiving is one-way in the UI.

A conversation shows only a participant list with presence dots.

A create-channel dialog opens from a `plus` beside the `CHANNELS` heading. Fields are Name, Slug prefilled by slugifying the name until the user edits it, and an optional Topic. The control renders when `state.me.is_admin || state.settings.allow_member_channels`, and is absent rather than disabled otherwise, because a control that will fail is worse than an absent one. On a `settings` frame that removes the permission while the dialog is open, close it and toast `An administrator restricted channel creation`.

### 4.6 Threads

The pane header carries `Thread`, the channel it belongs to as a link, and the reply count, with mute and close as icons. The root renders in a distinct block above an `N replies` divider, fetched when it is not in the client's map rather than printing `The parent message is not loaded`. Replies carry the same hover actions as the timeline minus Thread, and the pane composer gains the attach control and the mention autocomplete the main composer has.

### 4.7 Direct messages and the user roster

Replace the inline checkbox panel with the picker modal, opened from the same `+` and from `Ctrl+K` and `Cmd+K`. Sort existing conversation partners first, then alphabetically. An empty query shows the ten most recent partners.

Handle the `users` array in `ready` and the `user` frame, and merge the admin create response into `state.users`. A user created while a tab is open is currently absent from the picker and from mention autocomplete until a full reload, and any message they send renders with the author `Unknown`.

### 4.8 Admin

Split the five stacked cards into four sections behind the pill rail, each linkable at `/admin/{section}` and loading only on activation. `loadAll` currently fetches everything on entry and again on every render.

| Section | Contents |
|---|---|
| Identities | Users and Agents, in that order |
| Channels | The channel table read from `GET /api/admin/channels` rather than `state.containers`, the create form, and the `Who can create channels` control |
| Invites | The create form, the one-time URL block, and the outstanding table |
| Stats | The stats grid and a retention card fed by `GET /api/admin/retention`, which has no caller today |

Every form field gets a visible label; a grid of bare placeholders tells the reader nothing once a value is typed. Row actions become icon buttons with `title`, hued per action. The filled `bg-mauve` button appears once per section, on the create action; destructive actions are ghost buttons in `text-red` that become a filled red button only inside the confirm modal. Archived rows render in `overlay1` with an `archived` badge. A retention value of `0` days renders as `Kept forever`.

The agent surface gains the two headers a daemon needs, rendered as a copyable snippet beside the claim token:

```
Authorization: Bearer <token>
X-Isane-Agent: <handle>
```

and a `Deregister` action calling `POST /api/admin/agents/{handle}/deregister`, which has no caller today.

### 4.9 User settings

A dialog opened from the sidebar footer user block, which is inert text today.

1. **Profile.** Avatar upload, display name, and read-only handle and email, writing `PATCH /api/auth/me`.
2. **Password.** Current, new, and confirm, writing `POST /api/auth/password`, which exists and has no caller. The client minimum is 8 characters, matching `minPasswordLength`.
3. **Notifications.** When push is unconfigured, one line reading `Push notifications are not configured on this server.` When permission is `denied`, one line reading `Your browser is blocking notifications for this site.` and no controls. Otherwise the enable button, a per-device toggle writing `PUT /api/push/subscribe/enabled`, and a `Forget this device` control writing `DELETE /api/push/subscribe`. Neither endpoint has a caller today.
4. **Sign out**, which also stays in the footer.

### 4.10 Accessibility

The tree contains zero `aria-*` attributes, zero `role` attributes, and zero `tabindex`. Fix at least:

- Message hover actions are `display: none` outside a hover, so Reply, Thread, Edit, and Delete are keyboard-unreachable with no other path to any of them.
- Login and invite fields have no label, no name, and no id, so password managers cannot fill them.
- `toast()` has no `role="status"`, so `Reconnecting` is never announced.
- `<time>` carries no `datetime`.
- The picker, the thread pane, and the call pane have no `role="dialog"`, no focus trap, no Escape handling, and no focus restoration.
- The page has no `<main>` landmark, and two `<h1>` elements are in the document at once because `#admin-root` is only hidden.
- Five fields carry no focus treatment at all, so keyboard traversal of the admin page is invisible.
- Placeholders use `overlay0` on `surface0` at 2.57:1, and they are the only label those fields have.

## 5. Out of scope

Named so the boundary is explicit rather than forgotten.

- Calls, voice, video, screen share, recording, and the LiveKit webhook event path. Noted in passing: the call button renders for every non-archived container even where LiveKit is unconfigured and pressing it can only fail, and any member can start a recording with no admin gate and no system message.
- Release machinery: `build-all`, `docker-push`, the `version` target, and a release workflow.
- A per-user default notification level, which `notification_prefs` has no row shape for.
- A server-side thread read marker. Thread unread is derived client-side from `thread_last_reply_at` against a `localStorage` timestamp, which is deliberately cheap and deliberately not authoritative.
- Agent job history in the admin area, which no endpoint lists.
- `Also send to #channel` from a thread.
- Message reactions, pins, and bookmarks.
- A members pane. The presence data exists and a token is assigned, but nothing in the feature set needs it.

## 6. Verification

Run against a local instance rather than the deployment.

```
make build
./isane serve -c <config> --debug
```

Every claim of a fix is exercised: a message sent and read back in the timeline without a `?m=` deep link, a thread reply appearing in the pane, an invite created from the admin page, a search result that navigates, an agent row showing a handle, a channel created by a non-admin with the setting on and refused with it off, a second browser seeing that channel without a reload, a theme toggle that survives a reload and re-themes Mermaid and code blocks, a sidebar that collapses and resizes, and the attachment authorization check refusing a non-member.

Check every surface at 390x844, 768x1024, and 1440x900, in both themes.
