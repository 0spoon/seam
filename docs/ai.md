# AI

Seam treats AI as a core tool, not a sidebar widget.

## Providers

Seam supports three LLM providers. Embeddings always run locally on Ollama (your vectors, your machine), but chat completions can come from wherever you want:

| Provider | Config | Good for |
|---|---|---|
| **Ollama** (default) | `llm.provider: "ollama"` | Privacy maximalists, people with beefy GPUs, not paying per token |
| **OpenAI** | `llm.provider: "openai"` | GPT-4o, or any OpenAI-compatible API (Azure, Together, Groq, etc.) |
| **Anthropic** | `llm.provider: "anthropic"` | Claude |

Switch providers with one config line. Mix and match -- local embeddings with cloud chat is the sweet spot for most setups.

## Features

**Ask Seam** -- Chat with your notes. Ask a question, get an answer grounded in things you actually wrote, with citations. It embeds your query, retrieves relevant chunks from ChromaDB, and streams a response with full conversation history.

**Semantic Search** -- "What did I write about caching strategies?" works even if you never used the word "caching." Embedding-based similarity search with optional recency bias.

**Synthesis** -- "Summarize everything I know about project X." Seam pulls up to 50 relevant notes and generates a cross-note synthesis. Available as a regular response or SSE streaming.

**Auto-Link Suggestions** -- On save, Seam reads your note, finds semantically similar content, and suggests wikilinks you probably should have added.

**Writing Assist** -- Select text and ask AI to expand a bullet into a paragraph, summarize a wall of text, or extract action items into a checklist. Three modes: `expand`, `summarize`, `extract-actions`.

**Tag & Project Suggestions** -- AI reads your note content and suggests tags from your existing taxonomy and which project it belongs to.

**Related Notes** -- Every note shows semantically similar notes in a sidebar. The connections you did not know you had.

**Voice Transcription** -- Record audio, Whisper transcribes locally, AI auto-summarizes. No audio leaves your machine.

## Task Queue

All AI work runs through a priority queue with fair round-robin scheduling across users. Interactive requests (chat, writing assist) jump the line. Background tasks (embeddings, auto-linking) wait politely. Tasks survive server restarts. Configurable workers, timeouts, and retries.

## Default Models

| Role | Default Model | Swappable? |
|---|---|---|
| Embeddings | `qwen3-embedding:8b` | Yes, any Ollama model |
| Chat | `qwen3:32b` | Yes, or use OpenAI/Anthropic |
| Background tasks | `qwen3:32b` | Yes, or use OpenAI/Anthropic |
