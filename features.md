# Feature Ideas

20 features to make Seam a better knowledge system -- prioritized from an agent's perspective.

## Status Key

- [ ] Not started
- [x] Complete
- [~] In progress

---

## 1. Reflexive Knowledge Distillation

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** High

Agents accumulate session findings but they stay flat text. Add a background process that periodically reviews completed session findings, extracts recurring patterns, and auto-promotes them into structured knowledge notes with cross-references. This turns throwaway session logs into a growing, compounding knowledge base without manual curation.

**Why it matters for agents:** Every agent session currently writes findings that get buried. Distillation means future sessions inherit progressively richer context from the briefing system, making each agent invocation smarter than the last.

---

## 2. Semantic Deduplication and Merge Suggestions

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Use embedding similarity to detect near-duplicate notes and present merge suggestions in the review queue. Show a diff-style view and let users (or agents) choose which content to keep, merge, or discard.

**Why it matters for agents:** Agents frequently create notes that overlap with existing ones (especially via capture or memory_write). Without dedup, the knowledge graph gets noisy and RAG context quality degrades.

---

## 3. Temporal Context Windows for RAG

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Add time-decay weighting to semantic search results. Recent notes should score higher than old ones when relevance is similar. Expose a `recency_bias` parameter on `context_gather` and `notes_search` MCP tools so agents can tune how much they care about freshness.

**Why it matters for agents:** Current RAG retrieval treats a note from today and one from six months ago equally. For tasks like "what have I been working on" or "current status of X", recency is a critical signal.

---

## 4. Structured Task/Action Tracking

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Extract and track actionable items (`- [ ]` checkboxes) across all notes into a unified task view. Parse checkbox state from markdown, surface overdue/stale tasks, and let agents query open actions scoped by project or tag.

**Why it matters for agents:** Agents doing "extract-actions" via the writer produce action items that vanish into note bodies. A queryable task index lets agents follow up, prioritize, and report on outstanding work across the entire knowledge base.

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

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Let users and agents register webhook rules: "when a note is created in project X with tag Y, call this URL" or "when a note links to note Z, notify me." Expose via MCP as `webhook_register` / `webhook_list` / `webhook_delete`.

**Why it matters for agents:** Agents today are pull-based -- they only see the world when invoked. Event subscriptions enable reactive agent workflows: auto-summarize new captures, auto-link new notes, trigger reviews when projects grow past a threshold.

---

## 8. Multi-Hop RAG with Citation Chains

- [ ] **Status: Not started**
- **Effort:** Large
- **Impact:** High

Extend the chat system to follow wikilinks from retrieved chunks and pull in linked context (up to N hops). Return citation chains showing the reasoning path: "Found in Note A, which links to Note B, which contains the answer."

**Why it matters for agents:** Single-hop RAG misses knowledge that's distributed across connected notes. A user's understanding of a topic is often spread across 3-5 linked notes. Multi-hop captures this structure.

---

## 9. Agent Playbooks (Reusable Session Templates)

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Define reusable session blueprints: a named sequence of steps (tools to call, notes to read, knowledge to gather) that an agent can instantiate. Stored as special notes with `type:playbook` tag. MCP tools: `playbook_list`, `playbook_get`, `playbook_instantiate`.

**Why it matters for agents:** Agents repeat common workflows (weekly review, project onboarding, inbox triage). Playbooks codify these into repeatable, improvable recipes instead of relying on prompt engineering each time.

---

## 10. Smart Inbox Triage

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Auto-classify new inbox notes: suggest project placement, tags, and related notes. Run as a background AI task when notes land in inbox. Present suggestions in the review queue with one-click accept.

**Why it matters for agents:** The inbox is the primary capture point (URL capture, voice, quick notes). Without triage, it becomes a dumping ground. Automated classification keeps the knowledge base organized with minimal user effort.

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

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** High

Let users schedule recurring agent operations: "every Monday, synthesize notes from last week," "daily, triage inbox," "every Friday, generate project health report." Stored as cron entries in the user DB, executed by the task queue.

**Why it matters for agents:** Knowledge maintenance is a continuous process. Scheduled tasks turn Seam from a passive store into an active knowledge partner that maintains itself.

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

- [ ] **Status: Not started**
- **Effort:** Medium
- **Impact:** Medium

Let users and agents query structured aspects of the knowledge base in natural language: "how many notes did I create last week?", "which projects have no activity in 30 days?", "what are my most-linked notes?". Translate to SQL/search queries behind the scenes.

**Why it matters for agents:** Agents can already search content but can't easily answer meta-questions about the knowledge base itself. NL-to-query bridges the gap between unstructured knowledge and structured metadata.

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

Allow users to define custom MCP tools as small Go plugins or scripts (shell, Python) that the server loads at startup. Each plugin declares its tool schema, and the MCP server auto-registers it. Enables domain-specific integrations without forking the core.

**Why it matters for agents:** Every user's knowledge workflow is unique. A plugin system lets agents access domain-specific tools (Jira lookup, calendar integration, code repo search) through the same MCP interface, making Seam the universal agent memory layer.

---

## Priority Matrix

| # | Feature | Effort | Impact | Agent Value |
|---|---------|--------|--------|-------------|
| 1 | Reflexive Knowledge Distillation | Large | High | Critical |
| 2 | Semantic Deduplication | Medium | High | High |
| 3 | Temporal RAG Windows | Medium | High | High |
| 4 | Structured Task Tracking | Medium | High | High |
| 5 | Knowledge Graph Clustering | Large | Medium | Medium |
| 6 | CRDT Concurrent Editing | Large | Medium | Medium |
| 7 | Webhooks & Event Subscriptions | Medium | Medium | High |
| 8 | Multi-Hop RAG | Large | High | Critical |
| 9 | Agent Playbooks | Medium | High | Critical |
| 10 | Smart Inbox Triage | Medium | High | High |
| 11 | Note Maturity Scoring | Small | Medium | High |
| 12 | Project Health Dashboard | Medium | Medium | Medium |
| 13 | Annotation Layer | Medium | Medium | Medium |
| 14 | Scheduled Agent Tasks | Medium | High | Critical |
| 15 | Provenance Tracking | Medium | Medium | High |
| 16 | Diff-Aware Versioning | Medium | Medium | High |
| 17 | Cross-User Sharing | Large | Medium | Medium |
| 18 | NL Query Interface | Medium | Medium | Medium |
| 19 | Reading List & Spaced Repetition | Medium | Medium | Medium |
| 20 | Plugin/Extension System | Large | High | Critical |

### Recommended Build Order (highest agent ROI first)

1. **Smart Inbox Triage** (#10) -- quick win, immediate value
2. **Note Maturity Scoring** (#11) -- small effort, enables gardening
3. **Diff-Aware Versioning** (#16) -- schema exists, just needs wiring
4. **Temporal RAG Windows** (#3) -- improves every agent interaction
5. **Structured Task Tracking** (#4) -- unlocks action-oriented workflows
6. **Agent Playbooks** (#9) -- codifies repeatable agent patterns
7. **Scheduled Agent Tasks** (#14) -- makes the system self-maintaining
8. **Reflexive Knowledge Distillation** (#1) -- the compounding flywheel
