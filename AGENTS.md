# Repository Agent Rules

## Scope

These rules apply to implementation work in this repository. Read-only questions do not require implementation quality gates or project-context writes.

<!-- begin:repository-memory-bank-rule:start -->
# Global Memory Bank Rules

These rules are mandatory for every task and every response in every repository.

## Memory location and project identity

- The shared memory root is `/private/tmp/ai-agent/memory`.
- Use the current Git repository root directory name as `projectName`. If there is no Git root, use the current working directory name.
- Never store secrets, credentials, tokens, private keys, or raw sensitive values in memory.

## Required pre-response load

Before doing task work or composing any user-facing answer, load all memory files for the current project.

1. Use the installed Memory Bank CLI first and load the project in one command: `memory-bank-cli memory_bank_read_all --project <projectName>`. Do not run preliminary `list_projects` or `list_project_files` commands during the normal path.
2. After agent context compaction completes successfully, immediately reload every Memory Bank file for the current project before continuing task work or composing a response. Use the same CLI-first fallback sequence defined in this section.
3. Every Memory Bank CLI or MCP operation has a hard 30-second limit. Do not retry a failed or timed-out operation in the same turn.
4. If the CLI is unavailable, times out, or fails, fall back to the `memory-bank` MCP server. Call `list_project_files`, then issue the required `memory_bank_read` calls concurrently rather than serially.
5. If both CLI and MCP fail, read every file directly from `/private/tmp/ai-agent/memory/<projectName>/`.
6. If the project has no memory yet, continue the task and initialize it during the required persistence step.

## Required end-of-turn persistence

The final operation before sending each final answer must persist all durable context needed to continue later: the user's goal and constraints, decisions and rationale, changes made, commands/tests and outcomes, current state, blockers, and next steps.

1. Prefer the CLI: use `memory-bank-cli memory_bank_update` for existing files and `memory-bank-cli memory_bank_write` for new files. Do not run preliminary list commands.
2. Every Memory Bank CLI or MCP operation has a hard 30-second limit. Do not retry a failed or timed-out operation in the same turn.
3. If a CLI write fails, fall back to `memory_bank_update` or `memory_bank_write` through MCP.
4. Only if both CLI and MCP fail may you write directly under `/private/tmp/ai-agent/memory/<projectName>/`.
5. Keep `activeContext.md` current and append or update `progress.md`; update other memory files when their durable facts change.
6. Because no tools can run after a final response is emitted, perform this persistence immediately before the final response. Do not claim persistence unless the write succeeded.

## Verbatim final-response persistence

- For every response, persist the complete final answer verbatim in `/private/tmp/ai-agent/memory/<projectName>/lastResponse.md`.
- `lastResponse.md` must contain exactly the final answer shown to the user, from its first character through its last character, preserving all Markdown, code blocks, links, punctuation, whitespace, and line breaks.
- Never replace the verbatim response with a summary, paraphrase, context note, progress report, or selected highlights. Updates to `activeContext.md` and `progress.md` are supplementary and do not satisfy this requirement.
- Before emitting the final answer, first finish drafting it as an immutable payload. Persist that exact payload using the CLI-first fallback chain, then emit the same payload without any further edits. This ordering is required because tools cannot run after the final answer has been emitted.
- Overwrite `lastResponse.md` on every response so it always contains the most recent complete assistant final answer.
- A response is not ready to send until its exact payload has been persisted successfully. Do not claim that the verbatim response was saved unless the write succeeded.

## MCP tool contract

- `memory_bank_read`: read a memory bank file.
- `memory_bank_write`: create a memory bank file.
- `memory_bank_update`: update an existing memory bank file.
- `list_projects`: list available projects.
- `list_project_files`: list files within a project.
<!-- end:repository-memory-bank-rule:end -->

## Session identity and context

Before implementation work:

1. Establish one stable Session ID outside the repository at `<OS_TEMP_DIR>/ai-agent-sessions/<conversation-key>/session-id`.
2. Store its path in `AI_SESSION_ID_FILE` and reuse the same Session ID for the whole conversation.
3. Read every existing file under `.context/`.
4. Use exactly `.context/session-<SESSION_ID>.md` as the writable implementation context. Never create a second context file for the same Session ID.
5. Pass the exact Session ID, ID-file path, and context-file path to any participating subagent. Subagents must not create or select context files independently.

For a read-only repository task, do not write `.context/`. If writable state is genuinely required, keep it outside the repository beside the external Session ID.

## Discovery and planning

- Read relevant repository documentation and agent skills before changing files.
- Inspect Git status and preserve unrelated user changes.
- Reuse existing code and configuration; do not introduce equivalent duplicates.
- Use GitNexus for impact analysis when the repository has a current GitNexus index. Warn before changing a high- or critical-risk symbol.
- For broad or risky changes, write a concrete implementation plan before coding.

<!-- begin:agent-rule-boundary-rule:start -->
### Agent rule boundaries

- Every new rule added to `AGENTS.md` must be enclosed in a uniquely named boundary using exactly this structure:

  `<!-- begin:<rule-name>:start -->`
  `<!-- end:<rule-name>:end -->`

- The opening marker must appear immediately before the rule section, and the matching closing marker must appear immediately after it.
<!-- end:agent-rule-boundary-rule:end -->

<!-- begin:tooling-rule-protection:start -->
### Tooling rule protection

- Never modify, disable, weaken, remove, or replace tools or configuration used to check, lint, test, validate, analyze, format, build, or otherwise enforce code quality.
- Never bypass these tools or their rules in code, configuration, scripts, commands, Git hooks, or implementation structure. Suppressions, exclusions, ignored paths, wrapper commands, conditional skips, and equivalent workarounds are prohibited.
- Code must closely follow the intended architecture and flow enforced by the existing rules. Do not create arbitrary paths, modules, packages, dependencies, or imports merely to make an implementation pass.
- In most cases, enforcement rules and their tooling must not be changed. Refactor or correct the implementation instead.
- If compliance is genuinely impossible after the implementation has been completed:
  1. Explain clearly to the user which rule would need to change, why the implementation cannot comply, and the consequences of changing it.
  2. Stash the entire implementation with a specific, descriptive stash message.
  3. Stop and wait for the user to decide whether the rule may be changed.
  4. If the user rejects the rule change, reapply the stash and refactor or correct the implementation until it complies. Do not bypass the rule.
  5. If the user explicitly approves the rule change, reapply the stash and implement the approved rule change together with the original code.
- Never change a protected rule or its tooling without the user's explicit approval through this process.
<!-- end:tooling-rule-protection:end -->

<!-- begin:implementation-approval-rule:start -->
### Implementation specification and approval gate

- Before implementing a change or modifying any project file, write a concrete implementation specification directly in the chat.
- The specification must contain a language-native code sketch, not only a prose summary or high-level plan.
- Include every module and directory that will be created or changed, their exact proposed names and paths, their responsibilities and relationships, and representative code for the important types, interfaces, functions, methods, configuration, and control flow.
- Include the tests that will be created or changed and describe the behavior each test will verify.
- After presenting the specification and code sketch, stop and wait for the user to approve it explicitly.
- Until that approval is received, perform read-only discovery only. Do not create, edit, delete, rename, format, generate, install, or otherwise mutate project files, dependencies, Git state, or external systems.
- If the user requests revisions, update the specification in the chat and wait for explicit approval again before implementation.
- After the approved implementation is complete, run `make check-all`, fix every failure, and repeat the command until it passes. Do not report the implementation as complete unless the final run succeeds.
<!-- end:implementation-approval-rule:end -->

## Implementation

- Write code and technical documentation in English.
- Keep business logic out of `main.go`; use it only as the composition entry point.
- Place feature code under `internal/<feature>/` and use `domain`, `application`, `infra`, and `delivery` layers only when they are actually needed.
- Domain packages must not depend on delivery frameworks, persistence implementations, process environment, or transport serialization.
- Prefer the standard library until a dependency provides a concrete, justified benefit.
- Batch naturally multi-item operations and avoid N+1 calls or queries.
- Do not add CI/CD, Docker, deployment, or infrastructure files unless the user explicitly requests them.

## Quality gates

Before finishing an implementation task, run and fix failures from:

```bash
make check-all
```

This includes formatting, unit tests, race tests, vet, golangci-lint, layout checks, architecture checks, duplicate detection, and build checks. Do not claim a check passed unless it ran successfully.

Inspect all changed and non-ignored untracked files before committing. Confirm that no secrets, credentials, generated artifacts, or unrelated changes are included.

When GitNexus is configured, run change detection before committing and refresh the index after the commit.

## Git

- Use commitlint-compatible Conventional Commits: `<type>(<scope>): <description>`.
- Common types are `feat`, `fix`, `refactor`, `test`, `docs`, `build`, and `chore`.
- Keep the subject imperative, lowercase, concise, and without a trailing period.
- Never rewrite or discard user-owned Git history or changes unless explicitly requested.

<!-- begin:commit-rule:start -->
### Commit rules

- Write every commit message entirely in English.
- Use the Conventional Commit format `<type>(<scope>): <description>`.
- Always include a non-empty scope and a concise, meaningful description.
- Before every commit, allow the Husky `pre-commit` hook to run `make check-all`; do not bypass the hook with `--no-verify`.
- Allow the Husky `commit-msg` hook to validate the message with commitlint.
<!-- end:commit-rule:end -->

## Documentation and final context

Keep `README.md` and relevant project documentation synchronized with the implemented code. Replace stale descriptions instead of appending contradictory notes.

Before the final response, update the current `.context/session-<SESSION_ID>.md` with:

- what was implemented and why;
- files changed and resources reused;
- commands run, failures encountered, and fixes applied;
- remaining risks or follow-up work;
- reusable lessons for the next task.

Verify that the external Session ID still matches, only one context file exists for it, and no unauthorized context file was created.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **phan-quyen-golang** (87 symbols, 125 relationships, 4 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/phan-quyen-golang/context` | Codebase overview, check index freshness |
| `gitnexus://repo/phan-quyen-golang/clusters` | All functional areas |
| `gitnexus://repo/phan-quyen-golang/processes` | All execution flows |
| `gitnexus://repo/phan-quyen-golang/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
