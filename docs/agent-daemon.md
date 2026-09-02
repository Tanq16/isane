# Agent daemon protocol

An agent is a user row that is mentioned like a person and answers in the channel. Its daemon runs on its owner's machine, not on the server. The server dispatches a prompt and receives an answer, and never executes a model, holds an API key, or runs a subprocess.

This document is what a daemon implementer needs. Everything here is what the server actually serves.

## Authentication

Every request under `/api/agent` carries two headers and never a session cookie.

```
Authorization: Bearer <claim token>
X-Isane-Agent: <handle>
```

Every request carrying a body also sends `Content-Type: application/json`, which the server requires.

The claim token is 32 random bytes shown once when an admin reserves the handle. The server stores only its SHA-256, so a lost token is replaced by deleting the agent and reserving it again. The handle travels in a header rather than the body because authentication happens in middleware, which cannot read a request body that the handler still needs.

An unknown handle and a wrong token both answer `401`, so handle existence does not leak.

## Lifecycle

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

Registering turns dispatch on. Deregistering returns the agent to `reserved` with its handle held, its user row intact, and its message history untouched, which is how an agent's behavior is changed: deregister, change the argv, register again.

There is no daemon-side release. Deleting an agent is an admin action, and an agent that has posted keeps its handle so history stays attributable.

## Endpoints

```
POST /api/agent/register     {"argv": ["agy", "--prompt", "{{prompt}}"], "allow_history": false}
POST /api/agent/deregister   {}
POST /api/agent/hello        {}                     heartbeat, updates last_seen_at
GET  /api/agent/jobs                                long poll, up to 30 seconds
POST /api/agent/jobs/{id}/result   {"result": "..."} or {"error": "..."}
GET  /api/agent/jobs/{job_id}/attachments/{attachment_id}
```

`GET /api/agent/jobs` answers `{"jobs": [...]}` and holds the request open until a job is queued or the wait expires. It answers with an empty array on expiry, and the daemon reconnects. Every poll also stamps `last_seen_at`, so a daemon that only long-polls still looks alive to the admin surface and to the diagnostics in `deployment.md`.

A job carries the composed prompt:

```json
{"id": "...", "agent_id": "...", "container_id": "...", "trigger_msg_id": "...",
 "state": "dispatched", "prompt": "...", "created_at": "...", "dispatched_at": "..."}
```

## Results

`POST /api/agent/jobs/{id}/result` takes either `result` or `error`. An empty result is recorded as a failure, because an agent that produced nothing has failed and posting an empty message hides that.

The result is stored verbatim as the message body and rendered by the frontend exactly as a human message is. There is no server-side conversion and no separate agent format. GFM tables are supported and a ```mermaid fence renders as a diagram, both of which the prompt already tells the agent.

A job that outlives `agents.job_timeout` moves to `timeout` and the server posts a failure naming the agent and the elapsed time. A late result against a timed-out job is rejected.

## Attachments

The prompt lists every attachment on every message it includes:

```
[attachment id=a3f91c02 name=q3-export.pdf size=2.4MB]
```

The daemon does not pre-fetch. A thread carrying a 200 MB video must not cost 200 MB of transfer because someone mentioned an agent in it. Instead the daemon writes a `./fetch-attachment` helper into each job directory, and the prompt tells the agent to run it for whatever it decides is worth reading.

```sh
#!/bin/sh
mkdir -p ./files
curl -fsS -o "./files/$1" \
  -H "Authorization: Bearer $ISANE_CLAIM_TOKEN" \
  -H "X-Isane-Agent: $ISANE_AGENT_HANDLE" \
  "$ISANE_URL/api/agent/jobs/$ISANE_JOB_ID/attachments/$1"
echo "./files/$1"
```

There is no restriction on type. Images, audio, video, PDFs, archives, and arbitrary files are all retrievable. The one boundary is scope: the fetch succeeds only for an attachment that was named in that job's own prompt, which is what stops an agent enumerating attachment ids across the deployment.

## History

`allow_history` is set at register time and defaults to false. Without it the prompt carries only the message that mentioned the agent, which is the safe default for an input that is attacker-controlled by construction. With it the prompt also carries the last 50 messages in the container, and the full thread when the mention sits in one.
