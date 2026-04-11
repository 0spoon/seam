---
name: seam-onboard
description: "One-time setup to teach Claude about Seam MCP. Asks whether to install the Seam-awareness block globally (~/.claude/CLAUDE.md) or into the current project (./CLAUDE.md), writes a marker-wrapped block, then self-removes."
user-invocable: true
disable-model-invocation: true
argument-hint: ""
---

You are installing Seam awareness into a CLAUDE.md so future Claude Code instances automatically know about the user's personal knowledge system and its MCP tools. After successfully installing, you remove yourself so you do not clutter future sessions.

## What you are installing

A block inside `CLAUDE.md` that tells a future agent:

- Seam is a local-first personal knowledge system with persistent memory and notes, exposed via MCP at tools prefixed `mcp__seam__`.
- **Memories and notes are shared across every Claude instance the user runs** — other agents working on the same project (and on other projects, for global memory) read and write the same store. Treat `memory_write` and `notes_create` like writing to a shared team wiki.
- Default posture: use Seam for non-trivial work only; skip it for trivial edits and throwaway tasks.

## Workflow

### 1. Pre-flight: confirm Seam MCP is registered (and register it if not)

First, check whether Seam is already registered with Claude Code and, if so, at which scope. You want it at **user** scope so it is available in every project, not just the current directory. `claude mcp add` defaults to `local` and will silently drop the registration into `.mcp.json` of whichever directory you happen to be in — always pass `--scope user` explicitly (see 1d).

```bash
claude mcp get seam 2>/dev/null | awk -F': ' '/^  Scope:/{print $2; exit}'
```

Interpret the output:

- **`User config`** — already registered at user scope. Continue to step 2.
- **`Project config (shared via .mcp.json)`** or **`Local config ...`** (any non-user scope) — wrong scope. Tell the user you found Seam registered at the wrong scope and will migrate it to user scope. Remove the existing entry first, then fall through to step 1a to re-register it correctly:
  ```bash
  claude mcp remove seam
  ```
  (No `-s` flag — `claude mcp remove` removes from whichever scope the entry lives in.)
- **Empty output** — not registered. Fall through to step 1a.

To (re-)register it, you need a static bearer token (API key). **Do not paste a placeholder** — try to auto-discover the real key from the user's machine in this order. You already have tool access, so use it directly instead of shelling everything.

#### 1a. Check the environment

```bash
printf '%s' "${SEAM_MCP_API_KEY:-}"
```

If the output is non-empty, record it as `api_key` and jump to 1d.

#### 1b. Find the Seam checkout via the installed service

`make install-service` bootstraps seamd with its working directory set to the Seam repo root. Pull that path out of the service definition:

- **macOS (launchd):**
  ```bash
  plutil -extract WorkingDirectory raw -o - "$HOME/Library/LaunchAgents/com.seam.seamd.plist" 2>/dev/null
  ```
- **Linux (systemd --user):**
  ```bash
  awk -F= '/^WorkingDirectory=/{print $2; exit}' "$HOME/.config/systemd/user/seamd.service" 2>/dev/null
  ```

If neither returns a path, the service is not installed — skip to 1c.

Otherwise treat that path as `REPO_ROOT` and read `$REPO_ROOT/seam-server.yaml` with your `Read` tool. From it, extract two values:

- `mcp.api_key` — the static bearer token. It is under the top-level `mcp:` key in the yaml. If present and non-empty, record it as `api_key`.
- `listen` — the server's bind address (e.g. `":8080"` or `"127.0.0.1:8080"`). Record it as `listen`.

#### 1c. Ask the user only if auto-discovery failed

If you still have no `api_key` after 1a and 1b, ask the user exactly once:

> I could not auto-discover your Seam MCP API key. Either paste the key here, or tell me the absolute path to your Seam checkout (the directory that contains `seam-server.yaml`) and I'll read it from there.

If they give a path, read `seam-server.yaml` from that path and extract `mcp.api_key` and `listen` as in 1b.

If after all of that you still cannot find a key, stop and tell them:

> Seam MCP is not registered, and I could not find a `mcp.api_key`. Generate one with `openssl rand -hex 32`, put it under `mcp.api_key` in `seam-server.yaml` (or export it as `SEAM_MCP_API_KEY`), restart seamd, and re-run `/seam-onboard`.

#### 1d. Register the server

Compute the MCP URL from `listen`:

- If `listen` is empty or starts with `:` (bind-all), use `http://localhost${listen}/api/mcp`, defaulting to `http://localhost:8080/api/mcp` when `listen` is empty.
- Otherwise use `http://${listen}/api/mcp`.

Then register Seam with Claude Code at **user scope** so it is available in every project (not just the directory you happen to be in right now). The `--scope user` flag is required — `claude mcp add` otherwise defaults to `local`, which ties the registration to the current project only. Substitute the real values (do **not** leave placeholders):

```bash
claude mcp add --scope user --transport http seam "<mcp_url>" --header "Authorization: Bearer <api_key>"
```

Report the URL you registered (but not the raw key — just say "using the discovered API key" or "using the key you provided") and confirm the command succeeded before moving on. If the `claude mcp add` command fails, surface the error to the user and stop.

### 2. Interview (one question only)

Ask exactly this, and wait for the answer:

> Where should I install Seam awareness?
>
> 1. **Global** — `~/.claude/CLAUDE.md`. Every Claude Code session in every project will know about Seam.
> 2. **This project** — `./CLAUDE.md` in the current working directory. Only this project.

Do NOT ask any other questions. The posture is hard-coded to "use Seam for non-trivial work only".

### 3. Prepare the target

- If **global**: target is `$HOME/.claude/CLAUDE.md`. Create it (empty) if missing.
- If **project**: target is `./CLAUDE.md` in the current working directory. If it does not exist, tell the user where you are (`pwd`) and ask whether to create a new `CLAUDE.md` there. If they decline, stop without writing anything.

Check whether the target file already contains the line `<!-- seam-onboard:start -->`. If it does, tell the user Seam awareness is already installed in that file and ask whether to replace the existing block. If they confirm, remove everything from `<!-- seam-onboard:start -->` through `<!-- seam-onboard:end -->` (inclusive) before writing the new block.

### 4. Write the block

Append this exact content to the target file, preserving the start/end markers so the skill can safely re-run later:

```markdown

<!-- seam-onboard:start -->
## Seam (personal knowledge system)

Seam is a local-first personal knowledge system with persistent memory, notes, tasks, and a knowledge graph, exposed to you via MCP tools prefixed `mcp__seam__`. **Memories and notes are shared across every Claude instance the user runs** — other agents working on this project (and on other projects, for global memory) read and write the same store. Treat `mcp__seam__memory_write` and `mcp__seam__notes_create` like writing to a shared team wiki: be specific, dated, and useful to a future agent who will lack your current context.

**Use Seam for non-trivial work. Skip it for trivial edits, throwaway scripts, and things already covered by the codebase or the current conversation.**

When the task is non-trivial:

- Call `mcp__seam__session_start` at the start to get a briefing (recent activity, relevant memories, open tasks). Call `mcp__seam__session_end` when done to persist findings for the next agent.
- Use `mcp__seam__memory_write` / `mcp__seam__memory_read` for knowledge that should survive beyond this conversation (architectural decisions, debugging insights, gotchas, user preferences about their notes).
- Use `mcp__seam__notes_search` or `mcp__seam__context_gather` before asking the user to find things in their knowledge base — it is faster and more accurate than making them look.
- Use `mcp__seam__notes_create` to capture durable work output (research findings, meeting summaries, decision records). Agent-created notes are auto-tagged `created-by:agent`.
- For systematic debugging investigations, use the research lab tools (`lab_open`, `trial_record`, `decision_record`, `trial_query`) so parallel agents can collaborate on the same lab.

Do not duplicate what is already in the codebase or what the user just told you — read files and use conversation context first; reach for Seam when the information needs to outlive this conversation or cross between agents.
<!-- seam-onboard:end -->
```

Make sure there is a blank line before `<!-- seam-onboard:start -->` when appending to a non-empty file.

### 5. Confirm with the user

Report:

- The absolute path of the file that was written.
- Whether the block was newly added or replaced an existing one.
- The number of lines added.

### 6. Self-remove

Finally, delete this skill so it does not sit around in future sessions:

```bash
rm -rf "$HOME/.claude/skills/seam-onboard"
```

Tell the user:

> Seam onboarding complete. The `/seam-onboard` skill has been removed. To re-run it later (for example, to onboard another project), run `make install-onboard-skill` from the Seam repo.
