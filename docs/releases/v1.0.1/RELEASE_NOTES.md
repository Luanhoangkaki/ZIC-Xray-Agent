# v1.0.1-audit3-test — installer bugfix Pre-release

TEST ONLY / OBSERVE MODE / VLESS SUBSET / NOT PRODUCTION READY.

PATCH increases from 1.0.0 to 1.0.1 for installer fixes, following docs/versioning.md. This build replaces the unpublished v1.0.0-audit3-test-r1 preparation; published v1.0.0-audit2-test and its assets remain immutable. The owner authorized this experimental Pre-release if every mandatory local gate passes. Any FAIL/mismatch must stop publication.

- Agent release identity is centralized in internal/buildinfo/version.go. CLI version, HTTP User-Agent, installer help, BUILD.json, archive/downloader filenames, source/build manifests and fixed-tag URLs identify v1.0.1-audit3-test / base v1.0.1.
- Typed fixed stage/reason diagnostics distinguish credential input, Panel probe, adapter/config/TLS, users/policy, offline core and save failures. Raw errors, authenticated URLs, responses, credentials or customer data are not printed. The exact failure stage of the old VPS report remains unconfirmed.
- New archives explicitly record Xray/agent/entry scripts and directories as 0700, data 0600. Downloader checks outer and inner SHA256 before chmod and execute checks. The old Windows-built mode 0666 became 0600 under umask 077; no old checksum applies to this release.
- Four prompts remain protocol choice → Panel URL → visibly entered API Key → Node ID. Normal read -r; no silent read, program echo, key in argv/exported environment, extra prompt or automatic credential retry. Failed preflight preserves its code through rollback.
- TLS guard, observe default, VLESS subset, auth/policy, Stats accounting, durable UNKNOWN/no automatic retry, rollback, sandbox/resource/log limits and bounded HTTP/workers/state are preserved. Certificate TLS, unsupported protocols/transports/routes/encryption/policies are rejected. Real traffic sending and production manage remain disabled.
- Official separate Xray-core v26.10.29 stays checksum-pinned and runs only offline validation through a private verified copy. No installed Xray configuration/service or VPS Lab/production change occurs in publication.
- Builds use CGO_ENABLED=0, -buildvcs=false, -trimpath and stripped symbols. BUILD.json records the exact source commit and manifest. Independent clean-checkout fresh-cache builds must match both packaged agents byte for byte.

Required local validation: native Go with pinned official Windows core, 12 test packages / 82 top-level tests / 68 installer failure subcases / one Linux-only SKIP; installer/startup/rollback/guard/downloader/archive mocks; 15 Bash syntax checks; vet Windows/amd64 and Linux/amd64/arm64; module integrity; source/extracted archive secret-pattern scans; exact member/mode/ELF/hash checks; independent source–binary verification. Read the matching receipts for actual PASS status.

NOT RUN: Linux execution on both architectures; real Unix modes/noexec; Debian TTY/systemd/DynamicUser/LoadCredential/start/restart/reboot/update/rollback; whole-host privacy/journal/coredump/swap; separate agent/Xray/system CPU/RSS/FD/I/O on a real 1 vCPU/1 GB VPS; 10/20/30 connections, recovery/network/soak; race detector and dependency CVE scan. MSYS executable checks are explicit mocks plus archive headers. RSS <20 MB and idle CPU <1% remain unmeasured targets. No production-readiness or absolute-losslessness claim.

Exactly seven public assets: AMD64 and ARM64 v1.0.1 installer archives, their two .sha256 files, get-zic-xray-agent-v1.0.1.sh, its .sha256 and SHA256SUMS. Source archive, test executables, raw receipts, runtime/credentials/config/state/ledger/backups, .cache/.tools/.git/panel-reference and private keys are excluded. Source is committed using an explicit reviewed allowlist; fake source fixtures are not included in installer executables.
