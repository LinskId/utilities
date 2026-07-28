# Write a Test Plan

Generate a comprehensive E2E test plan for a Jira ticket or feature. Follow the structure and quality bar set by `test-plans/FLPATH-3254-dcm-authentication-test-plan.md`.

## Instructions

1. **Gather context** — Fetch the Jira ticket (use `@jira-access`), find the implementation PR(s) in `dcm-project` repos, and review the code diff to understand exact behavior (HTTP status codes, response shapes, error messages, edge cases).

2. **Check existing coverage** — Search `tests/e2e/` for any existing tests that partially cover the feature. Note what exists and what's missing.

3. **Write the plan** — Save to `test-plans/FLPATH-XXXX-<slug>.md` using the structure below.

## Required Sections

### Header Table

```markdown
| Field | Value |
|---|---|
| **Ticket** | [FLPATH-XXXX](https://redhat.atlassian.net/browse/FLPATH-XXXX) |
| **Author** | (use `git config user.name` or fall back to `$USER`) |
| **Version** | 1.0 |
| **Last Updated** | <YYYY-MM-DD> |
| **Status** | Draft |
```

### Description

- One paragraph explaining the feature under test
- **References** — link Jira tickets, implementation PRs, enhancement docs, OpenAPI specs
- **Acceptance Criteria** — copied or paraphrased from the ticket

### Environment and Global Setup

- Environment requirements (tools, ports, cluster access)
- Deployment configurations if multiple modes exist (table format)
- Global setup steps (clone, deploy, verify health)
- Helper commands (auth tokens, DB access, container discovery)

### Test Tiers

Table showing what's testable at each level:

| Tier | Requires | Test Cases | Notes |
|------|----------|------------|-------|
| Unit (in repo) | ... | ... | ... |
| E2E (existing) | ... | ... | ... |
| E2E (needed) | ... | ... | ... |

### Upstream Test Coverage

Document what the implementation repo already tests (unit, integration) so E2E tests don't duplicate that coverage.

### Test Cases

Each test case follows this structure:

```markdown
### TC-XX: <Descriptive title>

**Priority:** P1 (critical) | P2 (important) | P3 (nice-to-have)
**Type:** Functional | Negative | Security | Edge Case | Cleanup
**Method:** Automated (E2E) | Automated (subsystem) | Manual
**Labels:** `smoke`, `disruptive`, `cluster`, etc.
**Requires:** <Prerequisites or prior TCs>

#### Description

One paragraph explaining what this tests and why.

#### Prerequisites

- Bullet list of required state

#### Steps

**Step N: <Action description>**

\`\`\`bash
<exact command>
\`\`\`

**Expected:** <precise assertion — HTTP code, response body fields, state change>

#### Cleanup

<cleanup steps or "None">
```

### Not Testable at E2E Level

Table explaining scenarios that can't be tested externally and why.

### Implementation Notes

- Container names / discovery patterns
- Existing helper functions to reuse
- Suggested file name for new tests

### Coverage Summary

Matrix mapping acceptance criteria to test tiers and specific TCs:

| AC | Unit (repo) | E2E (existing) | E2E (needed) |
|----|-------------|----------------|--------------|
| 1. ... | ✅/❌ | TC-XX | TC-YY |

### Risk Observations

Numbered list of operational risks, known limitations, and trade-offs discovered during analysis.

### Version History

| Version | Date | Changes |
|---|---|---|
| 1.0 | YYYY-MM-DD | Initial test plan |

### Sanitization Notice

```markdown
This document is intended for sharing. The following rules apply:
- No credentials, tokens, API keys, or passwords - use placeholders
- No internal hostnames or IPs
- No PII
```

## Quality Checklist

- [ ] Every step has an **exact command** (not pseudocode)
- [ ] Every step has a **precise expected result** (HTTP code + body fields)
- [ ] Implementation details verified against actual PR diff (not assumed)
- [ ] Existing coverage identified — no redundant test cases
- [ ] P1 tests cover happy path + critical failure modes
- [ ] Negative tests cover all documented error responses
- [ ] Cleanup steps prevent test pollution
- [ ] Disruptive tests (infra stop/start) are clearly labeled
- [ ] Response shapes match the actual code (verified from source)
- [ ] Dependencies between TCs are documented in a graph or table
