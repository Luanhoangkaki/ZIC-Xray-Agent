# v1.0.1-audit3-test — publication checklist

Publication is authorized conditionally by the owner: proceed only after all mandatory local gates PASS. Stop and report any FAIL/mismatch; never overwrite a released tag/asset. No Lab/production changes.

1. Verify Semantic Versioning: bug fix PATCH v1.0.1, prerelease v1.0.1-audit3-test. CLI, User-Agent, installer, BUILD.json, manifests, filenames, downloader pins and release docs must agree.
2. Stage only COMMIT_FILES.txt. Every complete Git tree blob must match scanned/tested source and independent clean-checkout build inputs; both packaged agents must be byte-identical. BUILD.json source_commit must resolve to this exact source.
3. Require native Go tests, installer/downloader/archive/rollback/guard mocks, Bash syntax, vet three targets, module integrity, source/extracted pattern scans, exact archive allowlists, Unix 0700 headers, ELF targets and SHA256 sidecars/pins.
4. Preserve four prompts and visible normal API Key input; TLS/observe/VLESS subset/auth/policy/accounting/UNKNOWN/rollback/resources. Tests use fake fixtures only.
5. Recheck GitHub read-only: published v1.0.0-audit2-test tag/release/seven digests unchanged; new v1.0.1-audit3-test tag/release absent; remote parent unchanged. Never force push or reuse old hashes.
6. Upload exactly seven assets: two v1.0.1 installer archives, two archive .sha256 files, get-zic-xray-agent-v1.0.1.sh, its .sha256 and SHA256SUMS. No wildcard upload; exclude source archive, tests, runtime, receipts, private configs/keys and caches.
7. Push the verified source, create only the new annotated tag, then draft Pre-release (not Latest). Download all seven draft assets by authenticated asset ID and verify hashes before publishing. Ambiguous mutation errors stop the workflow; inspect state rather than blindly retrying.
8. Publish as Pre-release only after draft verification. Download all seven exact public HTTPS URLs without auth, verify hashes/digests/aggregate sums and source/tag mapping. Probe the one-line bootstrap URL and SHA using a harmless dispatch stub, never the real installer. Provide the new command only after verification.

Linux/systemd/VPS/real Unix permission/performance/privacy/soak/race/CVE items stay NOT RUN until actually executed. Experimental publication does not imply production readiness or measured RSS/CPU guarantees. Existing dependency/core licenses remain included; no project license is selected automatically.
