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

### 1. Pre-flight: confirm Seam MCP is registered

Run:

```bash
claude mcp list 2>/dev/null | grep -i seam || echo "SEAM_MCP_NOT_REGISTERED"
```

If the output contains `SEAM_MCP_NOT_REGISTERED`, tell the user:

> Seam MCP is not registered with Claude Code yet. Register it first, e.g.:
>
> ```
> claude mcp add seam --transport http http://localhost:8080/api/mcp
> ```
>
> (Adjust the host/port to match your `seam-server.yaml`.) Then re-run `/seam-onboard`.

Stop. Do not continue.

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
