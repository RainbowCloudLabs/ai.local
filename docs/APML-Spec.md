# AI Plan Modeling Language (APML) Specification

Inspired by the design philosophy of RAML (RESTful API Modeling Language), **APML** (`version: draft`) is a declarative specification language designed to model, route, and govern local AI gateway plans.

By defining an explicit `baseUri` (e.g., `https://ai.gateway` or `https://ai.company.com`), APML allows upstream applications to transparently redirect their AI API requests to this gateway infrastructure. APML provides structured abstractions through `quotas` and `providers` to dictate token budget enforcement boundaries and map upstream vendor request/response schemas.

Ingress URL paths declared with a leading slash (`/`) map directly onto these configured providers and quota matrices. For example, hitting `https://ai.local/claude` seamlessly triggers the underlying telemetry pipelines and plan restrictions specified for that channel.

> ⚠️ **Network Domain Warning (mDNS Conflict)**: Avoid using `.local` TLDs (e.g., `https://ai.local`) in your `baseUri`. Operating systems utilizing Multicast DNS (mDNS) will intercept `.local` requests and bypass conventional unicast DNS resolution, causing routing failures. It is strongly recommended to use custom top-level domains such as `ai.gateway` or a fully qualified internal corporate domain (e.g., `ai.company.com`).
> 🛡️ **Boundary Separation Constraint**: APML is explicitly restricted to routing topography and telemetry modeling. It **does not engage** with the physical storage, lifecycle, or management of sensitive API keys (Upstream Credentials). Key bindings are isolated within the secure Control-Plane Keystore backend.

## AI PLAN Description
```yaml
title: AI Gateway Plan
baseUri: https://ai.gateway
version: draft
plan_version: 2026Q3
```

* **`title`**: String (Required). Arbitrary descriptive identity or naming matrix assigned to this specific AI API gateway configuration setup (e.g., `AI Gateway Plan`)
* **`baseUri`**: String (Required). The target structural domain or local interface bound to the proxy entrance (e.g., https://ai.gateway or https://ai.company.com). Upstream applications redirect their base URLs here for transparent routing. Note: Avoid .local suffixes due to mDNS resolution constraints.
* **`version`**: String (Required). DSL layout specification version control. Set to `draft` to enable relaxed parsing compatibility during rapid architecture iterations.
* **`plan_version`**: String (Required). The operational versioning token for the current quota modeling matrix. Must strictly match the alphanumeric format `^[a-zA-Z][a-zA-Z0-9-]*$` (underscores are forbidden). Modifying this token automatically channels all runtime telemetry to an isolated database partition (`usage-{plan_version}.db`), ensuring historical accountability and eliminating memory cache friction.

## Quotas Layout (`quotas`)
The `quotas` section models the token consumption constraints enforced across the gateway's routing tubes. It allows you to define distinct tiered budget profiles (e.g., `small`, `medium`, `large`) that monitor and restrict both daily and monthly token velocity.

```yaml
quotas:
  unlimited:
    daily: 0
    monthly: 0
  small:
    mode: per_key
    daily: 10000
    monthly: 100000
  large:
    mode: shared
    daily: 200000
    monthly: 2000000
```
### Execution Modes (`mode`)

The `mode` attribute dictates how the gateway isolates and tracks token utilization across incoming requests:

* **`per_key` (Isolated Key Scope)**: 
  Budgets are evaluated independently for each internal proxy key (`sk-local-xxx`). If Key A and Key B both hit the same route, Key A's consumption has zero impact on Key B's remaining quota. This mode is ideal for individual client billing and precise multi-tenant auditing.
* **`shared` (Global Endpoint Scope)**: 
  Running token metrics are combined across all active proxy keys hitting that identical routing path. If Key A consumes the entire daily pool, Key B will be immediately rate-limited upon the next request. This mode is designed to protect shared team budgets or enforce a hard ceiling on a unified upstream corporate account.

### Budget Windows (`daily` / `monthly`)

The gateway tracks consumption velocity across two distinct chronological windows:

* **`daily`**: Specifies the maximum number of aggregated tokens (Input + Output combined) permitted within a single calendar day (resetting cleanly at `00:00:00` UTC).
* **`monthly`**: Specifies the maximum token budget permitted within a single calendar month.
* **🛡️ Unlimited Bypass (`0`)**: Setting a budget boundary explicitly to `0` designates an **unlimited budget profile**. When an incoming request targets an unlimited quota profile, the gateway bypasses the pre-flight verification loop entirely, maximizing raw performance on the forwarding path.

## Providers Layout (`provider`)
The `providers` dictionary defines the explicit structural format of HTTP requests and responses for both standard payload prompts and real-time streams. This declarative mapping allows the `ai.local` gateway to accurately inspect, parse, and process token telemetry across different AI vendor protocols.
```yaml
providers:
  claude:
    host: api.anthropic.com
    api_key_prefix: X-API-Key
    input_message: messages.content
    usage:
      input_tokens: usage.input_tokens
      output_tokens: usage.output_tokens
    streaming:
      mode: split
      input:
        chunk_type: message_start
        input_tokens: message.usage.input_tokens
      output:
        chunk_type: message_delta
        output_tokens: usage.output_tokens

  gemini:
    host: generativelanguage.googleapis.com
    api_key_prefix: X-Goog-Api-Key
    input_message: contents.parts.text
    usage:
      input_tokens: usageMetadata.promptTokenCount
      output_tokens: usageMetadata.candidatesTokenCount
    streaming:
      mode: last
      input_tokens: usageMetadata.promptTokenCount
      output_tokens: usageMetadata.candidatesTokenCount

  openai:
    host: api.openai.com
    input_message: messages.content
    usage:
      input_tokens: usage.prompt_tokens
      output_tokens: usage.completion_tokens
    streaming:
      mode: last
      input_tokens: usage.prompt_tokens
      output_tokens: usage.completion_tokens
      request_option:
        stream_options:
          include_usage: true

  local:
    host: http://localhost:11434
    usage:
      input_tokens: prompt_eval_count
      output_tokens: eval_count
    streaming:
      mode: last
      input_tokens: prompt_eval_count
      output_tokens: eval_count

```

### Network & Context Extraction Properties
* **`host`**: The absolute hostname or local interface endpoint of the upstream AI vendor (e.g., api.anthropic.com or a local instance like http://localhost:11434).
*  **`api_key_prefix`**: [Optional] Specifies a custom HTTP header identifier required by the vendor for authorization. If omitted, the gateway defaults to the standard Authorization: Bearer <API_KEY> layout. Custom schemes (e.g., X-API-Key or X-Goog-Api-Key) must be explicitly declared.
*  **`input_message`**: [Optional] A notation path pointing to the textual content of the prompt inside the incoming request body. When defined, the gateway enables pre-flight validation by estimating input tokens before forwarding.

### Standard Response Post-Accounting (usage)
The usage block maps the notation paths pointing to token allocation summaries inside a standard (non-streaming) JSON response from the upstream vendor:
* **`input_tokens`**: The JSON path locating the consumed prompt/input token count.
* **`output_tokens`**: The JSON path locating the generated completion/output token count.

### Streaming Telemetry Handling (streaming)
AI vendors frame token metrics over Server-Sent Events (SSE) streams differently. APML models these behaviors into two target execution modes via the mode attribute:
* **`mode: last`** (e.g., OpenAI, Gemini, Ollama): The gateway monitors the token stream until the final terminal chunk arrives. It then extracts the cumulative input and output token counts using the configured JSON paths on that terminal block.
  
* **`mode: split`** (e.g., Anthropic Claude): The vendor scatters token statistics across separate event frames throughout the lifecycle of the stream. The gateway reactively scans individual chunks as they pass through the data plane:
  * **`input`** : Intercepts metrics when the chunk type matches the specified **`chunk_type`** (e.g., message_start).
  * **`output`**: Intercepts metrics when the chunk type matches the designated **`chunk_type`** (e.g., message_delta).

* **`request_option`**: [Optional] Defines additional payload patches that the gateway must automatically inject into the outbound request body to force the vendor to emit telemetry data (e.g., forcing OpenAI to include the final usage chunk).

## Route Mapping
Route mapping defines the incoming URL paths for the gateway. By defining paths starting with a leading slash (`/`), you create operational channels under your `baseUri`. Each route acts as a logical bridge that binds a specific AI provider to a designated quota restriction.

```yaml
/claude:
  provider: claude
  quota: medium

/gemini:
  provider: gemini
  quota: medium

/nolimit:
  provider: claude
  quota: unlimited

/local:
  provider: local
  quota: large
```

### Routing Binding & Scope Integrity
* **Path Registration**: Every root-level path starting with / creates a unique local API endpoint (e.g., https://ai.local/claude). Applications target these endpoints transparently as drop-in replacements for original vendor base URLs.
* **`provider`**: Must reference a valid structural configuration declared in the providers layout. This ensures the data plane knows exactly how to read incoming prompts and intercept streaming chunk tokens for that route.
* **`quota`**: Must reference a valid policy profile declared in the `quotas` section to enforce the corresponding daily and monthly token limits on this route.
