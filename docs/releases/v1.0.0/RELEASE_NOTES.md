# ZIC-Xray-Agent v1.0.0-audit2-test — GitHub Pre-release preparation

**TEST ONLY · OBSERVE MODE · VLESS subset · NOT PRODUCTION READY.**

Experimental tag: `v1.0.0-audit2-test`; never use the official `v1.0.0` tag for this candidate. Asset filenames retain the base version `v1.0.0`; BUILD.json and ASSETS.json identify the test tag and candidate. This preparation creates local files only. Linux on both architectures, real TTY/systemd/reboot/rollback, host privacy, 1 vCPU / 1 GB measurements, 10/20/30 connections, network comparisons and at least 24h soak remain RELEASE_GATE HOLD.

**Local audit2 candidate for review only; not published. Not production-ready. Debian 12 systemd/installer acceptance is NOT RUN.** No GitHub push/tag/draft/release/upload is allowed before actual acceptance and owner approval; see RELEASE_GATE.md.

Initial release candidate for Debian 12 amd64 and arm64, built as one Go agent process with official Xray-core kept separate. Installer packages include an unmodified pinned official core ONLY for offline `run -test`; it is never installed/started as a proxy service.

- Interactive installer: Shadowsocks / Trojan / Hysteria / VLESS menu, then Panel domain, visibly entered API Key, Node ID. No additional interactive questions.
- **Only the implemented VLESS subset can install, in observe mode.** Shadowsocks, Trojan, Hysteria1 and Hysteria2 adapters are incomplete and explicitly rejected. Hysteria versions are identified from panel version, never equated automatically.
- Auth/node/config/users/policy and offline core checks precede credential publication. API Key is passed on stdin; config directory 0700/files 0600.
- DynamicUser, LoadCredential, sandbox, bounded resources/logs, validated sync intervals (default 60 seconds), fresh-install rollback and read-only startup checks.
- Downloader pinned to test tag v1.0.0-audit2-test embeds archive SHA256 pins and checks them before listing, extracting or executing. Default only stages verified files; `--install` is explicit and refuses known active Lab/Agent/Xray services.
- No traffic POST, production management, panel changes, database/Redis/queue changes or active Lab deployment. No RSS/CPU or absolute-losslessness guarantees.

Assets: two installer archives, one `.sha256` per archive, fixed-version downloader plus its `.sha256`, and aggregate `SHA256SUMS`. Install/update/rollback instructions and complete test/security reports accompany the review folder. Installer archives deliberately exclude test executables, runtime evidence, private certificates/keys and panel-reference.

Validation in this preparation: **PASS** for 11 native Go test packages on Windows with official Windows core; **PASS** for rootless installer/startup/rollback command mocks; **PASS** for static vet, cross-build and ELF/SHA inspection of 22 Linux test binaries. These are Windows execution/static results. Linux-specific tests, both Linux architectures, actual Debian systemd/TTY/install/update/reboot and VPS resource/connection acceptance remain **NOT RUN**. No Linux test is relabelled PASS from Windows results. Downloader artifact tests are recorded separately in DOWNLOAD_TEST_RESULTS.md and are required for READY_FOR_REVIEW.

Source contract: kutycma/v2b-zic `f778400907da4c47f0466abcab21030009e2770e`, read-only. Local working-tree snapshot is recorded in SOURCE_FILES.sha256. Audit2 includes fixed-code diagnostics, suppressed library logging, no JSON validation file on disk, unit stderr/core controls and idle-queue allocation optimization. No source commit/tag is invented for the uncommitted candidate; GitHub remains untouched. API Key visibly echoes only while typing on TTY, per owner request; the application never repeats it in diagnostics/logs.

See INSTALL.md, UPDATE_ROLLBACK.md, TEST_RESULTS.md, SECURITY_SCAN.json and PUBLISH_CHECKLIST.md. Download URLs in those files are future locations until the owner publishes the reviewed assets.
