# ai.local.cli Command Line Tool Reference

The `ai.local.cli` is a secure, cross-platform administrative client that communicates with the `ai.local` control plane via a TLS-protected gRPC channel. 

Administrators use this tool to dynamically inject upstream API credentials into memory, manage internal client routing keys, and inspect consumption analytics.

---

## 🌐 Global Host Configuration

By default, the CLI attempts to connect to `127.0.0.1:50051`. You can redirect it to a remote production gateway using either the explicit `-addr` flag or the system environment variable.

### Explicit Argument Flag

```bash
./ai.local.cli -addr 192.168.1.4:50051 route list
```

### Environmental Fallback (Recommended)

To bypass repeating the address flag in every command session, bind the address to your terminal stack:

* Linux / macOS (Bash/Zsh):

```bash
export AI_LOCAL_ADDR="192.168.1.4:50051"
./ai.local.cli route list
```

* Windows PowerShell:

```powershell
$env:AI_LOCAL_ADDR="192.168.1.4:50051"
.\ai.local.cli.exe route list
```

---

## 🛠️ Command Matrix & Usage Examples

### 1. Route Management

Fetches and prints all route active schemas defined inside the running APML configuration profile.

* Syntax:

```bash
./ai.local.cli route list
```

* Output Example:

```
URI             QUOTA           PROVIDER
/openrouter0    unlimited       openrouter
/openrouter100  small           openrouter
```

---

### 2. Quota Audit

Inspects declared quota tiers, tracking token limitations, aggregation intervals, and active enforcements.

* Syntax:

```bash
./ai.local.cli quota list
```

* Output Example:

```
NAME          DAILY     MONTHLY    MODE       STATUS
unlimited     0         0          SHARED     ACTIVE
small         50000     1000000    PER_KEY    ACTIVE
```

---

### 3. Keystore Matrix (Internal Key Provisioning)

#### List Keys

Displays all provisioned active cryptographic mappings currently residing in the cluster.

* Syntax:

```bash
./ai.local.cli key list
```

#### Add Key

Registers a remote provider API credential into the gateway memory and extracts a safe, masked internal key tracking sequence for client distribution.

* Syntax:

```bash
./ai.local.cli key add <route_path>
```

* Interactive Walkthrough:

```bash
$ ./ai.local.cli key add /openrouter100
Please input key: ****************************
Alias (allow empty): developer-team-alpha

Generated internal key successfully:
sk-local-v2026-b8a2-9f3c-88e1
```

#### Revoke Key

Physically deletes and invalidates an internal routing identity sequence from the gateway cache using its unique identifier tracking UUID.

* Syntax:

```bash
./ai.local.cli key del <key_uuid>
```

* Execution Example:

```bash
$ ./ai.local.cli key del 4e1a8b92-3c2f-41ad-b12e-9943f2a8a761
Successfully revoked key tracking target [4e1a8b92-3c2f-41ad-b12e-9943f2a8a761]
```

---

### 4. Metrics & Usage Analytics (Stats Engine)

Queries aggregated database token accounts from the SQLite WAL engine layer.

#### Date Range Bound Scan

Filters logs based on absolute UTC timeline coordinates.

* Syntax:

```bash
./ai.local.cli stats --start-date YYYY-MM-DD --end-date YYYY-MM-DD
```

#### Monthly Summary Breakdown

Generates historical billing reports for a specific calendar year.

* Syntax:

```bash
./ai.local.cli stats --monthly [--year YYYY]
```

#### High-Resolution Verbose Diagnostics

Traces the top consumers and heavy request logs inside the system.

* Syntax:

```bash
./ai.local.cli stats --verbose [--count N]
```

