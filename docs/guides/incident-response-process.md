# Incident Response Process

## Severity Levels
- **SEV1** — full outage or data loss, all hands, page immediately.
- **SEV2** — major feature degraded or partial outage, on-call + team lead.
- **SEV3** — minor/localized issue, on-call handles solo, no page needed outside business hours.

## Roles
- **Incident Commander (IC)** — owns the timeline and decisions, does not personally debug.
- **Ops lead** — drives the technical investigation and fix.
- **Comms lead** — updates stakeholders/status page on a fixed cadence (e.g. every 30 min for SEV1).

## During the incident
1. Declare the incident and assign IC as soon as impact is confirmed — don't wait for root cause.
2. Open a dedicated channel/thread; all decisions and timestamps go there, not DMs.
3. Mitigate first (rollback, scale up, failover), root-cause second. Don't debug in prod under pressure if a known-safe rollback exists.
4. Check `search_knowledge` for a matching runbook before improvising — someone may have hit this before.

## After the incident
1. Declare resolved once impact stops, not once root cause is fully understood.
2. Write a postmortem within 48 hours using `guides/postmortem-template.md`.
3. Call `write_knowledge` with the runbook — symptom, root cause, fix, prevention — so the next on-call doesn't repeat the investigation.
