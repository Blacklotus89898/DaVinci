# Postmortem Template

Use this structure for any SEV1/SEV2 postmortem. Keep it blameless — focus on systems and process, not individuals.

```markdown
# Postmortem: <title>

**Date**: <date>  **Severity**: <SEV1/2/3>  **Duration**: <start-end, total downtime>

## Summary
One paragraph: what broke, what the user-visible impact was, how it was resolved.

## Timeline (UTC)
- HH:MM — first alert/report
- HH:MM — incident declared, IC assigned
- HH:MM — mitigation applied
- HH:MM — resolved

## Root Cause
The actual mechanism of failure — not just "a pod crashed" but why the crash led to user impact.

## Impact
Who/what was affected, for how long, any data loss.

## What Went Well

## What Went Poorly

## Action Items
| Action | Owner | Due |
|---|---|---|
```
