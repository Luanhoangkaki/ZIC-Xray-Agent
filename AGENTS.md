# ZIC-Xray-Agent: mandatory project constraints

- Version policy: every bug fix increments PATCH; backward-compatible new functionality increments MINOR (reset PATCH); incompatible changes increment MAJOR (reset MINOR/PATCH). Changing only a prerelease suffix does not replace a required PATCH bump. Published versions/tags/assets are immutable. Current installer fix release is v1.0.1-audit3-test; preserve v1.0.0-audit2-test. See docs/versioning.md.

- Target Debian/Ubuntu, linux/amd64 and linux/arm64, 1 vCPU / 1 GB RAM.
- Use Go and one agent binary/process. No Python, Node.js, Java or PHP daemon; no embedded core, V2Node or substitute core. Use official Xray-core separately.
- Targets, pending real measurements: agent steady RSS <20 MB; idle average CPU <1% of one vCPU. Never present these as measured guarantees.
- Default synchronization 60 seconds; honor validated panel intervals. Reuse HTTP connections, conditional ETags, bounded bodies, timeouts and bounded retries. No busy loops or unbounded workers/caches.
- Preserve authentication, authorization, expiry, quotas and access limits. Reject unsupported required policy rather than silently bypassing it.
- Traffic must use Xray Stats API. Never promise lossless accounting without proving durable snapshots, recovery, panel deduplication and acknowledgement semantics. Do not blindly retry ambiguous traffic submissions.
- One systemd agent process; restart on failure, bounded resources and logs. Default warning/error. Never log credentials, full authenticated URLs, user records or private keys.
- Installation asks protocol choice, Panel URL, visibly entered Server Token, Node ID, in that order (user instruction 2026-10-31: normal read, no read -s). Never repeat the token in program output/logs, argv or exported environment. Default node_type=vless. No hardcoded panel, token or node ID.
- Validate UniProxy V1 authentication, node existence and returned configuration before committing credentials. Restrict configuration directory to 0700 and files to 0600.
- Do not modify Xray configuration, start/restart Xray or deploy to a production VPS without explicit approval. Writing local code and preparing reviewable artifacts is authorized.
- Acceptance needs separate agent/Xray/system CPU and memory measurements on a real 1 vCPU / 1 GB Debian/Ubuntu VPS, including 10, 20 and 30 concurrent connections and recovery faults. Windows build/tests do not constitute these measurements.
- The panel reference is read-only at panel-reference/. Production contract: kutycma/v2b-zic commit f778400907da4c47f0466abcab21030009e2770e. See docs/panel-api.md before changing API adapters. Do not assume Xboard or other forks share the same schema.
- Observe stays the default. The user authorizes implementing opt-in lab-only Xray synchronization and an agent-side traffic queue/sender policy without changing panel source, API, database, Redis or queue. Never activate real traffic sending or production management. An uncertain send must become durable UNKNOWN and must not automatically retry. Do not claim absolute losslessness or production readiness.
