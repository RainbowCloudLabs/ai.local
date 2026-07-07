# Use AI.local with Continue (VS Code)

This guide shows how to connect the **Continue** VS Code extension to **AI.local** via an OpenAI-compatible endpoint.

---

## Prerequisites

- VS Code installed
- Continue extension installed
- Reachable AI.local gateway (example: `https://ai.local/openai/v1`)
- A valid internal AI.local key (example: `sk-local-...`)
- A trusted TLS certificate for your gateway (recommended)

---

## 1) Open Continue config

In VS Code:

1. Open Continue settings/config
2. Edit your `config.yaml` (or your active Continue config file)

---

## 2) Example configuration

> Replace model names with those enabled by your AI.local routing policy.

```yaml
name: Main Config
version: 1.0.0
schema: v1

models:
  - name: OpenAI GPT-4.1
    provider: openai
    model: gpt-4.1-2025-04-14
    apiBase: https://ai.local/openai/v1
    apiKey: ${AI_LOCAL_API_KEY}
    roles:
      - chat
      - edit
      - apply
    defaultCompletionOptions:
      contextLength: 1047576
      maxTokens: 32768
      useLegacyCompletionsEndpoint: false

  - name: o3
    provider: openai
    model: o3
    apiBase: https://ai.local/openai/v1
    apiKey: ${AI_LOCAL_API_KEY}
    requestOptions:
      verifySsl: false
    roles:
      - chat
    defaultCompletionOptions:
      contextLength: 200000
      maxTokens: 100000
    capabilities:
      - image_input

  - name: OpenAI GPT-4.1 mini
    provider: openai
    model: gpt-4.1-mini-2025-04-14
    apiBase: https://ai.local/openai/v1
    apiKey: ${AI_LOCAL_API_KEY}
    requestOptions:
      verifySsl: false
    roles:
      - chat
      - edit
      - apply
    defaultCompletionOptions:
      contextLength: 1047576
      maxTokens: 32768
      useLegacyCompletionsEndpoint: false
```

---

## 3) Set API key via environment variable (recommended)

Do **not** hardcode keys in config files.

### macOS / Linux

```bash
export AI_LOCAL_API_KEY="sk-local-xxxxxxxxxxxxxxxx"
code .
```

### Windows (PowerShell)

```powershell
# Current terminal session only (effective immediately, recommended)
$env:AI_LOCAL_API_KEY="sk-local-xxxxxxxxxxxxxxxx"
code .

# Or persist for the current user (requires a new terminal session)
[Environment]::SetEnvironmentVariable("AI_LOCAL_API_KEY", "sk-local-xxxxxxxxxxxxxxxx", "User")
```

After setting the variable permanently, restart VS Code (or open a new terminal and run `code .`).

---

## 4) TLS / certificate notes

### Recommended (production)
Use a trusted internal CA or a valid certificate for `https://ai.local`.

### Local testing only
If you use self-signed certificates, SSL validation may fail.

Temporary options (not for production):
- VS Code launch from terminal:
  ```bash
  NODE_TLS_REJECT_UNAUTHORIZED=0 code .
  ```
- Continue model option (if supported in your version):
  ```yaml
  requestOptions:
    verifySsl: false
  ```

⚠️ Security warning: disabling TLS verification increases MITM risk.  
Use proper certificates in production.

---

## 5) Verify your setup

Run these in Continue:

1. **Chat**: ask a simple prompt
2. **Edit**: ask Continue to modify code
3. **Apply**: apply generated patch

If all three work, Continue is successfully routed through AI.local.

---

## 6) Governance validation checklist (AI.local)

After connection, verify governance behavior:

- [ ] Requests authenticate with internal keys
- [ ] Token/usage accounting is recorded per key
- [ ] Quota/rate-limit policy is enforced
- [ ] Model allowlist/blocklist is enforced
- [ ] Audit logs include key/model/route metadata

---

## 7) Common issues

### 401 Unauthorized
- Check `AI_LOCAL_API_KEY`
- Confirm key is active in AI.local keystore
- Ensure Continue reads environment variables from your current VS Code session

### 404 / model not found
- Model is not mapped in AI.local route/provider config
- Verify model string matches your gateway routing rules

### SSL certificate error
- Install trusted certificate chain for `ai.local`
- Use `verifySsl: false` only for temporary local testing

### Requests bypass AI.local
- Confirm `apiBase` is exactly `https://ai.local/openai/v1`
- Remove conflicting provider configs in Continue

---

## 8) Team rollout best practices

- Issue one internal key per user/team
- Enforce per-key token and rate limits
- Rotate/revoke internal keys regularly
- Keep provider keys only inside AI.local (never in IDE clients)
- Review top models/routes by token usage weekly

---

## 9) Minimal single-model config

```yaml
name: AI.local only
version: 1.0.0
schema: v1

models:
  - name: AI.local GPT-4.1 mini
    provider: openai
    model: gpt-4.1-mini-2025-04-14
    apiBase: https://ai.local/openai/v1
    apiKey: ${AI_LOCAL_API_KEY}
    roles:
      - chat
```
