---
name: Bug Report
about: Report a bug or unexpected behaviour in ai.local
title: "[Bug] "
labels: bug
assignees: ''
---

## Bug Description

A clear and concise description of what the bug is.

---

## Environment

| Field            | Value |
|------------------|-------|
| OS               |       |
| Deployment       | Binary / Docker |
| Commit / Version |       |

---

## Steps to Reproduce

1. 
2. 
3. 

---

## Expected Behaviour

What you expected to happen.

---

## Actual Behaviour

What actually happened.

---

## Debug Log

Enable debug mode and attach the relevant output:

```bash
ai.local.cli debug on
```

```
Paste log output here
```

---

## APML Config Snippet

If the issue is related to routing, quota, or provider configuration, paste the relevant section of your `ai.local.apml`.  
**Remove or mask any sensitive values** (API keys, internal hostnames).

```yaml
# Paste relevant APML snippet here
```

---

## Severity

- [ ] Critical – gateway is down or data is at risk
- [ ] High – core feature broken with no workaround
- [ ] Medium – feature degraded, workaround exists
- [ ] Low – minor or cosmetic issue

---

## Additional Context

Add any other context, screenshots, or request/response samples here.
