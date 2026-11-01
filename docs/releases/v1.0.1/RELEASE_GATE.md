# v1.0.1-audit3-test — release gate

Chủ repository ngày 2026-11-01 cho phép commit/push/tag và phát hành GitHub Pre-release thử nghiệm v1.0.1-audit3-test nếu tất cả kiểm tra local bắt buộc đạt. Nếu có FAIL hoặc sai lệch phải dừng và báo cáo; không bỏ qua gate hoặc tự ghi đè asset/tag. Đây là quyền phát hành thử nghiệm, không cấp quyền thay đổi VPS Lab/production.

Gate local: version Go/CLI/User-Agent/installer/docs/build/manifest phải thống nhất v1.0.1-audit3-test và base v1.0.1; source allowlist và complete Git tree khớp source đã test; rebuild độc lập checkout sạch khớp byte agent AMD64/ARM64; go tests, mocks, vet, secret-pattern scan source/extracted archive, ELF/members/0700/SHA/downloader phải PASS. Draft asset tải lại phải khớp trước publish; public asset tải lại phải khớp trước cung cấp lệnh cài.

Giữ bốn prompt, API Key normal visible read, TLS guard, observe mặc định, VLESS subset, policy/auth và accounting/UNKNOWN/no automatic retry, rollback và resource controls. Chỉ upload đúng bảy file public trong manifest; không upload source archive, private evidence, credentials, runtime, .cache/.tools/.git/panel-reference, ledger, backup hoặc private keys.

V1.0.0-audit2-test đã phát hành bất biến. Các candidate v1.0.0 chưa phát hành cũng giữ nguyên để đối chiếu. Fix mới bắt buộc tăng PATCH theo docs/versioning.md; suffix audit/r không thay cho PATCH.

Linux execution AMD64/ARM64, Unix mode/noexec thật, TTY/systemd/start/restart/reboot/update/rollback, whole-host privacy, VPS 1 vCPU/1 GB CPU/RSS/FD/I/O, tải 10/20/30 kết nối, recovery/network/soak, race và CVE scan chưa chạy phải ghi NOT RUN. Các gate production vẫn HOLD; không khẳng định RSS <20 MB/CPU <1% đã đo, production-ready hoặc losslessness tuyệt đối.
