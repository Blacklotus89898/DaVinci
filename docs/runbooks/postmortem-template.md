# postmortem template

## Blameless postmortem template and process

### What a postmortem is for here

Single-node-ish homelab, one operator + automation. The goal is **learning capture, not process**: what broke, what amplified it, what shortens detection/fix next time. Blameless = the *system* failed, not a person; "user was away" is a system property (no automation watched it).

### Template (copy into `runbooks/postmortem-YYYY-MM-DD-<slug>.md`)

```markdown
## <One-line title>

- **Severity**: <Sev1 outage of X / Sev2 degraded / Sev3 cosmetic>
- **Window**: <start> → <end> (detection lag: <how long it ran before anyone knew>)
- **Blast radius**: <what was down/degraded; what was unaffected>
- **Status**: <resolved / mitigated / open — pending what>

### Timeline (UTC, approximate where noted)
- <timestamp> — first failure signal (logs/metrics evidence)
- <timestamp> — detection (who/what noticed, lag)
- <timestamp> — diagnosis complete
- <timestamp> — mitigation applied
- <timestamp> — service restored / accepted state

### Root cause
<The technical chain. Name the mechanism, not just the symptom.>

### What amplified it
<Secondary failures, disk hazards, freeze effects on other systems.>

### What went well
<Detection that worked, runbooks that helped, data preserved.>

### Action items
- [ ] <concrete, owner: user|claude, state: pending|blocked|done> — each links to a KB runbook or roadmap item if it produced one

### Lessons that generalize
<One or two sentences max — the reusable part. Cross-link the detailed runbook.>
```

### Filing rules

- File within the quarterly drill cycle for any Sev1/Sev2; immediately for anything that wedged more than one system.
- Every action item names an owner and its current state; the quarterly drill reviews open items (an open item with no movement for 2 cycles gets escalated to the user or dropped explicitly).
- The incident's technical fix runbook (if any) is a SEPARATE entry — the postmortem links to it, never inlines procedures.
- Facts only; approximate timestamps marked approximate. No speculation presented as fact.
