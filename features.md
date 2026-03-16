# Feature Ideas

Features to make Seam a better knowledge system -- prioritized from an agent's perspective.

Features related to the smart personal AI assistant initiative have been moved to `AGENTIC_PLAN.md`. This file covers knowledge system improvements that are independent of or complementary to the agentic assistant work.

## Status Key

- [ ] Not started
- [x] Complete
- [~] In progress
- [->] Moved to AGENTIC_PLAN.md

## Changelog

- **2026-03-15**: Reconciled with AGENTIC_PLAN.md. Moved #1, #9, #10, #14, #18, #25, #26, #28 to agentic plan. Added cross-references on #2, #20, #22, #30.
- **2026-03-15**: Implemented #3 (Temporal RAG), #4 (Task Tracking), #7 (Webhooks). Added features #21-#30.

---

## 1. Reflexive Knowledge Distillation

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 2 (Episodic Memory) + Phase 5 (Event-Driven Automations)

---

## 2. Semantic Deduplication and Merge Suggestions

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High
- **See also:** AGENTIC_PLAN.md Phase 10.3 (Smart Suggestions) -- dedup suggestions are one category, but the full dedup + merge workflow is broader and stands alone.

Use embedding similarity to detect near-duplicate notes and present merge suggestions in the review queue. Show a diff-style view and let users (or agents) choose which content to keep, merge, or discard.

**Why it matters for agents:** Agents frequently create notes that overlap with existing ones (especially via capture or memory_write). Without dedup, the knowledge graph gets noisy and RAG context quality degrades.

---

## 3. Temporal Context Windows for RAG

- [x] **Status: Complete**
- **Effort:** Medium
- **Impact:** High

Add time-decay weighting to semantic search results. Recent notes should score higher than old ones when relevance is similar. Expose a `recency_bias` parameter on `context_gather` and `notes_search` MCP tools so agents can tune how much they care about freshness.

**Why it matters for agents:** Current RAG retrieval treats a note from today and one from six months ago equally. For tasks like "what have I been working on" or "current status of X", recency is a critical signal.

**Implementation:** Exponential decay with 7-day half-life in `internal/search/recency.go`. FTS and semantic search both support `recency_bias` parameter (0.0-1.0). Wired through agent service and MCP tools (`context_gather`, `notes_search`).

---

## 4. Structured Task/Action Tracking

- [x] **Status: Complete**
- **Effort:** Medium
- **Impact:** High

Extract and track actionable items (`- [ ]` checkboxes) across all notes into a unified task view. Parse checkbox state from markdown, surface overdue/stale tasks, and let agents query open actions scoped by project or tag.

**Why it matters for agents:** Agents doing "extract-actions" via the writer produce action items that vanish into note bodies. A queryable task index lets agents follow up, prioritize, and report on outstanding work across the entire knowledge base.

**Implementation:** New `internal/task` package with regex-based checkbox parsing, DB-synced task index, and toggle API that updates both DB and markdown file. REST API at `/api/tasks`. MCP tools: `tasks_list`, `tasks_summary`. Auto-syncs via file watcher on note create/modify.

---

## 5. Knowledge Graph Clustering and Topic Maps

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** Medium

Run community detection (e.g., Louvain) on the wikilink + semantic similarity graph to auto-discover topic clusters. Surface these as navigable "topic maps" in both the web UI and as an MCP tool (`topics_list`, `topic_detail`).

**Why it matters for agents:** Agents currently search by text or embedding but have no concept of the knowledge topology. Topic maps let an agent understand "what areas of knowledge exist" before diving into specifics, enabling better planning and gap analysis.

---

## 6. Conflict-Free Concurrent Editing (CRDT)

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** Medium

Replace the current "last write wins" model with a CRDT-based merge strategy for note bodies. When an agent and a user (or two agents) edit the same note, changes merge automatically instead of overwriting.

**Why it matters for agents:** Agents appending context or links to a note while a user is editing it in the web UI is a real scenario. CRDTs prevent silent data loss.

---

## 7. Conditional Webhooks and Event Subscriptions

- [x] **Status: Complete**
- **Effort:** Medium
- **Impact:** Medium

Let users and agents register webhook rules: "when a note is created in project X with tag Y, call this URL" or "when a note links to note Z, notify me." Expose via MCP as `webhook_register` / `webhook_list` / `webhook_delete`.

**Why it matters for agents:** Agents today are pull-based -- they only see the world when invoked. Event subscriptions enable reactive agent workflows: auto-summarize new captures, auto-link new notes, trigger reviews when projects grow past a threshold.

**Implementation:** New `internal/webhook` package with HMAC-SHA256 signed deliveries, event type filtering, delivery tracking, and SSRF-safe URL validation. REST API at `/api/webhooks`. MCP tools: `webhook_register`, `webhook_list`, `webhook_delete`. File watcher dispatches `note.created`, `note.updated`, `note.deleted` events.

---

## 8. Multi-Hop RAG with Citation Chains

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** High

Extend the chat system to follow wikilinks from retrieved chunks and pull in linked context (up to N hops). Return citation chains showing the reasoning path: "Found in Note A, which links to Note B, which contains the answer."

**Why it matters for agents:** Single-hop RAG misses knowledge that's distributed across connected notes. A user's understanding of a topic is often spread across 3-5 linked notes. Multi-hop captures this structure.

---

## 9. Agent Playbooks (Reusable Session Templates)

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 1 (Multi-Step Reasoning) + Phase 5 (Event-Driven Automations)

---

## 10. Smart Inbox Triage

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 5.2 (Built-in Automations)

---

## 11. Note Maturity Scoring

- [ ] **Status: Not started**
- **Effort:** Small
- **Impact:** Medium

Score each note on a maturity scale based on: word count, number of links (in/out), tag coverage, recency of edits, whether it has been reviewed. Surface as a field on notes and as a filter in search. Highlight "stub" notes that need expansion.

**Why it matters for agents:** Agents doing knowledge gardening need to know where to focus. Maturity scores make it trivial to find underdeveloped notes that would benefit from expansion, linking, or synthesis.

---

## 12. Project Health Dashboard

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Per-project analytics: note count over time, link density, orphan ratio, tag coverage, average note maturity, activity heatmap. Available in the web UI and as an MCP tool (`project_health`).

**Why it matters for agents:** Agents managing knowledge across projects need a way to assess which projects are well-maintained and which are neglected. Health metrics drive targeted gardening.

---

## 13. Annotation Layer (Inline Comments)

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Add a non-destructive annotation system: comments anchored to specific text ranges within a note. Stored separately from the note body so the markdown stays clean. Agents can annotate notes with suggestions, questions, or cross-references without modifying the original content.

**Why it matters for agents:** Agents currently must edit note bodies to add context. Annotations let agents enrich notes without changing the user's authored content, respecting ownership while still adding value.

---

## 14. Scheduled Agent Tasks (Cron-style)

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 3 (Scheduled Triggers and Daily Briefing)

---

## 15. Provenance Tracking and Source Chains

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Track how each note was created and modified: original source (manual, URL capture, voice, agent synthesis, etc.), which agent sessions touched it, what transformations were applied. Stored as metadata, queryable via MCP.

**Why it matters for agents:** When an agent encounters a note during RAG, knowing its provenance helps assess reliability. A note synthesized by an agent from three sources is different from a hand-written reflection. Provenance enables trust-aware retrieval.

---

## 16. Diff-Aware Note Versioning

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

The `note_versions` table exists but isn't wired up. Complete the implementation: snapshot on every save, expose version history in the web UI with inline diffs, and add MCP tools (`note_versions_list`, `note_version_restore`). Show who/what made each change (user vs. agent).

**Why it matters for agents:** Agents that edit notes need a safety net. Version history lets users review and revert agent changes, building trust. It also lets agents understand how a note evolved.

---

## 17. Cross-User Knowledge Sharing (Controlled)

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** Medium

Allow users to publish specific notes or projects to a shared namespace that other users on the same machine can discover and link to. Read-only by default, with explicit grant for write access. Shared notes appear in search results with a "shared" badge.

**Why it matters for agents:** On a multi-user machine, knowledge silos are wasteful. Controlled sharing lets agents surface relevant knowledge from other users (with permission), enabling collaborative knowledge building.

---

## 18. Natural Language Query Interface for Structured Data

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 1 (Agentic Loop with Tool Use) -- the assistant with tool access *is* the NL query interface

---

## 19. Reading List and Spaced Repetition

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Track which notes a user has read and when. Surface notes for re-reading using spaced repetition intervals (like Anki). Agents can add notes to the reading queue with priority. MCP tools: `reading_queue_add`, `reading_queue_next`, `reading_queue_list`.

**Why it matters for agents:** Knowledge that isn't revisited is forgotten. Spaced repetition ensures important notes resurface at optimal intervals, and agents can curate the queue based on relevance to current work.

---

## 20. Plugin/Extension System for Custom Tools

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** High
- **See also:** AGENTIC_PLAN.md Phase 7 (External Integrations) -- the integration framework covers built-in connectors, but a general plugin/extension system for user-defined tools is broader.

Allow users to define custom MCP tools as small Go plugins or scripts (shell, Python) that the server loads at startup. Each plugin declares its tool schema, and the MCP server auto-registers it. Enables domain-specific integrations without forking the core.

**Why it matters for agents:** Every user's knowledge workflow is unique. A plugin system lets agents access domain-specific tools (Jira lookup, calendar integration, code repo search) through the same MCP interface, making Seam the universal agent memory layer.

---

---

## 21. Agent Memory Namespaces

- [ ] **Status: Not started**
- **Effort:** Small
- **Impact:** High

Add hierarchical namespaces to agent memory (e.g., `project/backend/conventions`, `tool/git/patterns`). Support glob-pattern reads (`memory_list category=project/*`) and namespace-scoped deletion. Currently memory is flat key-value under a single category.

**Why it matters for agents:** As agents accumulate memory across many sessions, flat categories become unwieldy. Namespaces let agents organize learned knowledge into a tree structure that mirrors the project/topic hierarchy, making retrieval precise and cleanup targeted.

---

## 22. Context Budget Optimizer

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High
- **See also:** AGENTIC_PLAN.md Phase 2 (Long-Term Memory) -- the assistant's memory retrieval uses budget-aware context selection, but a general-purpose MCP tool for any agent is broader.

Add a `context_optimize` MCP tool that, given a token budget and a goal, returns the optimal subset of notes/memory/session context that maximizes relevance while fitting within the budget. Uses embedding similarity + recency + link proximity to rank and pack content.

**Why it matters for agents:** Agents have limited context windows. Currently they request context and hope it fits. A budget-aware optimizer lets agents say "give me the best 8000 tokens of context for this task" and get a curated, non-redundant selection.

---

## 23. Inline Code Block Execution

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Detect fenced code blocks in notes (```bash, ```sql, ```python) and allow execution via an MCP tool (`code_run`). Results are appended below the code block as a collapsible output section. Sandboxed with configurable allowed languages.

**Why it matters for agents:** Agents writing runbooks or data analysis notes can make them executable. A note documenting a database query becomes a live dashboard. Transforms notes from passive documentation to active tools.

---

## 24. Note Templates with Dynamic Placeholders

- [ ] **Status: Not started**
- **Effort:** Small
- **Impact:** Medium

Extend the existing template system with dynamic placeholders: `{{date}}`, `{{project}}`, `{{related_notes:query}}`, `{{task_summary:project}}`. Templates can pull live data when instantiated. Agents can create notes from templates via `note_from_template` MCP tool.

**Why it matters for agents:** Agents generating recurring reports (weekly review, project status) need structured output. Dynamic templates eliminate boilerplate and ensure consistent formatting while pulling fresh data at creation time.

---

## 25. Semantic Link Suggestions

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 5.2 (Built-in Automations) -- already partially implemented as the `autolink` AI task type

---

## 26. Session Handoff Protocol

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 2 (Episodic Memory + Conversation Summarization)

---

## 27. Knowledge Gap Detection

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** High

Analyze the knowledge graph for structural gaps: topics referenced but never documented, questions asked in chat but never answered in notes, wikilinks pointing to non-existent notes, projects with sparse coverage. Surface as a "gaps" report via MCP and web UI.

**Why it matters for agents:** Agents doing knowledge gardening need to know what's missing, not just what exists. Gap detection guides agents toward the highest-value knowledge creation: filling holes rather than duplicating coverage.

---

## 28. Conversation-to-Note Extraction

- [->] **Moved to AGENTIC_PLAN.md** -- Phase 1 (Tool Use) + Phase 2 (Episodic Memory)

---

## 29. Multi-Model Routing

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Route different AI tasks to different Ollama models based on task type: fast/small models for classification and tagging, medium models for search and summarization, large models for synthesis and generation. Configurable per-task routing table.

**Why it matters for agents:** Not every AI operation needs the biggest model. Smart routing reduces latency for simple tasks (inbox triage, tag suggestion) while reserving powerful models for complex work (synthesis, multi-hop reasoning). Better UX, lower resource usage.

---

## 30. Agent Confidence Scoring

- [ ] **Status: Not started**
- **Effort:** Small
- **Impact:** Medium
- **See also:** AGENTIC_PLAN.md Phase 10.1 (Feedback Loop) -- confidence scoring feeds into the feedback system, but the scoring mechanism itself is a standalone knowledge system feature.

Add confidence metadata to agent outputs: when an agent writes a memory entry, creates a note, or provides a search result, include a confidence score (0-1) based on source quality, embedding similarity, and recency. Low-confidence items get flagged for human review.

**Why it matters for agents:** Not all agent outputs are equally reliable. Confidence scoring lets the system (and users) distinguish between high-certainty facts and speculative connections, enabling trust-calibrated workflows and targeted review.

---

## Priority Matrix

Features remaining in this file (excluding completed and items moved to `AGENTIC_PLAN.md`):

| # | Feature | Effort | Impact | Agent Value | Status |
|---|---------|--------|--------|-------------|--------|
| 2 | Semantic Deduplication | Medium | High | High | Not started |
| 3 | Temporal RAG Windows | Medium | High | High | **Complete** |
| 4 | Structured Task Tracking | Medium | High | High | **Complete** |
| 5 | Knowledge Graph Clustering | Large | Medium | Medium | Not started |
| 6 | CRDT Concurrent Editing | Large | Medium | Medium | Not started |
| 7 | Webhooks & Event Subscriptions | Medium | Medium | High | **Complete** |
| 8 | Multi-Hop RAG | Large | High | Critical | Not started |
| 11 | Note Maturity Scoring | Small | Medium | High | Not started |
| 12 | Project Health Dashboard | Medium | Medium | Medium | Not started |
| 13 | Annotation Layer | Medium | Medium | Medium | Not started |
| 15 | Provenance Tracking | Medium | Medium | High | Not started |
| 16 | Diff-Aware Versioning | Medium | Medium | High | Not started |
| 17 | Cross-User Sharing | Large | Medium | Medium | Not started |
| 19 | Reading List & Spaced Repetition | Medium | Medium | Medium | Not started |
| 20 | Plugin/Extension System | Large | High | Critical | Not started |
| 21 | Agent Memory Namespaces | Small | High | High | Not started |
| 22 | Context Budget Optimizer | Medium | High | Critical | Not started |
| 23 | Inline Code Block Execution | Medium | Medium | Medium | Not started |
| 24 | Dynamic Note Templates | Small | Medium | Medium | Not started |
| 27 | Knowledge Gap Detection | Large | High | High | Not started |
| 29 | Multi-Model Routing | Medium | Medium | Medium | Not started |
| 30 | Agent Confidence Scoring | Small | Medium | Medium | Not started |

Moved to AGENTIC_PLAN.md: #1, #9, #10, #14, #18, #25, #26, #28

### Recommended Next Build Order (highest agent ROI first)

1. **Agent Memory Namespaces** (#21) -- small effort, organizes growing memory
2. **Note Maturity Scoring** (#11) -- small effort, enables gardening
3. **Context Budget Optimizer** (#22) -- every agent interaction gets smarter
4. **Semantic Deduplication** (#2) -- keeps knowledge graph clean
5. **Multi-Hop RAG** (#8) -- captures distributed knowledge across linked notes
6. **Knowledge Gap Detection** (#27) -- guides high-value knowledge creation
7. **Provenance Tracking** (#15) -- enables trust-aware retrieval
8. **Plugin/Extension System** (#20) -- makes Seam extensible for any workflow
9. **Agent Confidence Scoring** (#30) -- calibrates trust in agent outputs
10. **Diff-Aware Versioning** (#16) -- safety net for agent edits
