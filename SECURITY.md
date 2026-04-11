# Security Policy

Seam is a local-first, single-user system. The threat model and the hardening rules enforced in the code live in [`docs/security.md`](docs/security.md). This document covers **how to report a vulnerability** and what to expect after you do.

## Supported versions

Seam does not yet ship tagged releases. Security fixes are applied to the `main` branch only. If you are running an older commit, update to the latest `main` before reporting -- the issue may already be fixed.

| Version | Supported |
| ------- | --------- |
| `main`  | Yes       |
| Older commits | No  |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security problems.** Public reports can expose other users before a fix is available.

You have two private reporting channels. Either one is fine; pick whichever is easier for you.

**Email (preferred for sensitive details):**

- Send to **security@thereisnospoon.org**.
- Include the information listed under "What to include" below.
- If you want end-to-end encryption, say so in the first message and we will arrange a key exchange.

**GitHub private vulnerability reporting:**

1. Go to the repository's **Security** tab: <https://github.com/0x3k/seam/security>
2. Click **Report a vulnerability**.
3. Fill out the advisory with a description, reproduction steps, affected version/commit, and impact.

Either channel reaches the maintainers privately. GitHub advisories have the advantage of a built-in fix/credit workflow; email is better if you prefer to keep the report off GitHub entirely.

## What to include

A useful report usually has:

- A clear description of the vulnerability and its impact.
- Steps to reproduce, ideally against a fresh `make init && make run` setup.
- The commit hash you tested against.
- Any proof-of-concept code, HTTP requests, or payloads.
- Your assessment of severity (informational / low / medium / high / critical) and reasoning.

## What to expect

Seam is maintained by a small team (often one person). We will do our best to:

- **Acknowledge** your report within 7 days.
- **Assess and triage** within 14 days, and share our reproduction status with you.
- **Fix** confirmed vulnerabilities on `main` as quickly as severity warrants. Critical issues get priority; lower-severity issues may be batched.
- **Credit** you in the advisory and release notes if you want credit. Anonymous reports are welcome.

Please give us a reasonable window to fix the issue before public disclosure. Coordinated disclosure works best for everyone.

## Scope

**In scope:**

- The `seamd` server (Go backend, REST API, WebSocket hub, MCP endpoint).
- The `seam` TUI client.
- The web frontend under `web/`.
- The `seam-onboard` Claude Code skill and other artifacts shipped from this repository.
- Build and install scripts (`Makefile`, `scripts/`).

**Out of scope:**

- Vulnerabilities in third-party dependencies (Ollama, ChromaDB, Docker, etc.) unless Seam is using them in an unsafe way. Report upstream first, and open a Seam issue only if we need to change how we call them.
- Attacks that require the attacker to already have code execution or filesystem access as the Seam owner on the same machine. Seam is a single-user local system; root on the box is game over by design.
- Denial of service against a single-user instance on loopback. Seam's request hardening (`docs/security.md` > "Request hardening") exists to keep the process healthy, not to withstand a dedicated attacker on the same machine.
- Best-practice suggestions with no exploit path ("you should use algorithm X instead of Y"). File those as regular issues.

## Known gaps

`docs/security.md` > "Known gaps" lists intentional non-invariants (for example, the single-user schema means per-resource ownership is not enforced at the SQL layer). Reports that rediscover a documented known gap will be closed with a pointer to that section, unless you have found a way to exploit it that the doc does not cover.

## Thank you

We appreciate the time it takes to find and report security issues responsibly. Thank you for helping keep Seam and its users safe.
