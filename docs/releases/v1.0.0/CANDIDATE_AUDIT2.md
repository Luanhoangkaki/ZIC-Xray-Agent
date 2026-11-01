# Candidate v1.0.0-audit2 — review cục bộ, chưa phát hành

Ngày 2026-10-31, Asia/Saigon. Candidate mới ở `dist/release-candidates/v1.0.0-audit2/`; không ghi đè hoặc sử dụng archive cũ tại `dist/release/v1.0.0/`. Tên archive vẫn theo tên asset v1.0.0 dự kiến; checksum trong thư mục candidate mới mới là checksum của audit2. Không có push, tag, GitHub draft/Release/upload hoặc thao tác dịch vụ/VPS.

## Nội dung đã tích hợp

- API Key Panel nhập bằng normal `read -r`, ký tự hiển thị trên TTY theo yêu cầu mới. Đúng bốn prompt: protocol → Panel → API Key Panel → Node ID. Không in lại key trong output/lỗi, không truyền qua argv/exported environment; stdin tới preflight. Thư mục config 0700, file 0600; DynamicUser/LoadCredential và sandbox/resource caps giữ nguyên.
- Diagnostic cố định, chặn stdlib/gRPC log thô; main panic không in giá trị/stack. Log allowlist/rotation giữ giới hạn và không có trường dữ liệu truy cập khách hàng. Test canary kiểm cả file xoay và gRPC fatal path, không in canary vào evidence.
- Tiền kiểm core bằng bản official pin SHA riêng, JSON qua stdin; không ghi file JSON tiền kiểm chứa UUID/private key, không đọc config core đang chạy. Core stdout/stderr bị bỏ. Unit templates stderr/stdout null, traceback/core controls được giữ; chưa nghiệm thu trên systemd thật.
- Idle queue tránh clone map/cấp phát khi không có delta; không đổi durable accounting, UNKNOWN hoặc retry semantics. HTTP reuse/ETag/bounded bodies/timeouts, worker/map/queue limits và log rotation giữ nguyên. Không thêm xử lý vào đường truyền VPN, không thay TLS/encryption/transport để giảm tài nguyên.
- Runner Debian kiểm official core SHA trước execution và từ chối cả năm service Xray/Agent/Lab đang active. Guard logic kiểm bằng mock; không triển khai Lab hay gọi systemctl thật.
- Chỉ VLESS subset được phép đi qua preflight để cài observer. Shadowsocks/Trojan/Hysteria1/Hysteria2 tiếp tục từ chối; không suy từ REALITY config/preflight thành handshake hoặc hiệu năng VPN đã được nghiệm thu.

## Kiểm thử và số đo thật của đợt này

| Kiểm tra | Kết quả / phạm vi |
| --- | --- |
| Full Go native, official Windows core | 11 package; 73 test cấp cao PASS, 1 `/proc` test SKIP cần Linux |
| Installer UI/credential/rollback | Git Bash mock PASS, normal read, đúng bốn prompt, không key trong xtrace/output/argv/environment; chưa TTY thật |
| Startup readiness/PID/restart/rollback và Debian runner guards | Mock PASS; không phải systemd thật |
| Bash syntax | 13 script PASS |
| go vet | Windows/amd64, Linux/amd64, Linux/arm64 PASS |
| Build mới | 6 tool và 22 test binary Linux cross-build; thực thi Linux NOT RUN |
| Packaging/downloader/secret scan | Chỉ gắn PASS sau khi `verify-release-candidate.ps1` hoàn tất; xem PACKAGE_CHECK, SECURITY_SCAN, SOURCE_SECURITY_SCAN và READY_FOR_REVIEW của đúng candidate |
| CPU/RSS/SSD/FD thực tế; network và long soak trên VPS 1 vCPU / 1 GB | NOT RUN |

Benchmark cùng Windows/amd64, Go 1.26.5, i3-13100, GOMAXPROCS=8, 3 lần `benchtime=1s`, median:

| Idle counters | Baseline trước audit (lượt trước) | Audit2 lượt này | Cấp phát trước → audit2 |
| --- | ---: | ---: | --- |
| 5.000 | 265458 ns/op | 38810 ns/op | 524801 → 0 B/op; 19 → 0 allocs/op |
| 20.000 | 1319087 ns/op | 155816 ns/op | 2098627 → 0 B/op; 67 → 0 allocs/op |

Baseline lấy từ `.cache/audit-v1.0.0/evidence/enqueue-before.log`, audit2 từ `.cache/release-v1.0.0-audit2/evidence/enqueue-current.log`. Đây là microbenchmark control plane, không phải RSS/CPU%/VPS hoặc throughput. Hai bản không ghi idle queue; không có bằng chứng SSD writes giảm từ thay đổi này. Chưa có phép đo trước/sau toàn Agent/Xray.

Test process HTTPS mock 10 warmup + 300 poll: goroutine 3 → 3, HeapAlloc sau GC 410800 → 441512 byte. Đây là test process ngắn trên Windows, không phải daemon/Xray live và không chứng minh không leak trong 24h. Linux FD/I/O chưa đo. Mục tiêu Agent RSS <20 MB, idle CPU <1% vẫn chưa được chứng minh.

## Privacy còn giới hạn và các điều kiện HOLD

Không thu access/DNS/website/packet/customer session logs. Tuy nhiên config giữ API Key, panel config/snapshot có thể giữ UUID/private key, ledger giữ định danh Stats/user ID và byte accounting; các state này nhạy cảm và cần quyền tối thiểu. Không xóa hoặc bỏ checkpoint để giả lập “không có định danh trên đĩa”. Rollback giữ config private; temp checkpoint có thể còn sau SIGKILL, chưa có cleanup/retention được nghiệm thu. Snapshot vẫn fsync cả khi 304 để giữ LastSuccess/freshness. Chưa mã hóa toàn bộ state tại rest hoặc đo SSD wear.

Journal lịch sử, Xray gốc, host coredump/swap/backup và panel/reverse-proxy log chưa được kiểm trên VPS; không tuyên bố toàn host không có dữ liệu nhạy cảm. Token query UniProxy giữ đúng API contract, Agent không log URL; không đổi panel hay bảo vệ TLS. Các yêu cầu privacy toàn host, Linux cả hai kiến trúc, TTY/systemd/reboot/update/rollback thật, race/leak/soak và kiểm ping/jitter/throughput trước/sau vẫn HOLD. Goroutine live metric chưa triển khai; không dùng stack/heap dump hoặc pprof public để thay thế.

Xem [runbook Debian riêng](../../audits/debian12-privacy-measurement.md) và [RELEASE_GATE](RELEASE_GATE.md). Không chạy installer, restart/reboot hoặc tạo tải trên active Lab/production. Chưa xin xác nhận phát hành vì còn nghiệm thu thực tế.

## Bộ review và cách tái tạo

Các script chỉ chạy local, không có lệnh push/tag/Release:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-release-candidate.ps1 -Candidate audit2
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prepare-release.ps1 -Candidate audit2
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-release-candidate.ps1 -Candidate audit2
```

Thư mục candidate tồn tại thì packager từ chối ghi đè. Khi cần build lại, dùng candidate ID mới, chạy cả ba script cùng ID và cập nhật tài liệu/evidence tương ứng trước đóng gói. Không thay byte đã review.

Hai installer và mỗi `.sha256`, downloader ghim hai hash mới cùng `.sha256`, aggregate SHA256SUMS là bộ asset dự kiến. `zic-xray-agent-source-v1.0.0-audit2.tar.gz` và `.sha256` là source review riêng, không phải installer/public asset tự động. Source allowlist gồm Go/scripts/docs/units/module files, không `.git`, `.tools`, `.cache`, panel-reference, runtime, credentials, cert/private key hay backup. Source test có dữ liệu giả được review; không có test executable/runtime fixture trong installer. Pattern scan có giới hạn, không là chứng minh tuyệt đối.

Source artifact có `source_commit=null` vì working tree chưa commit; hash manifest đối chiếu source thực tế, không giả mạo commit/tag. Sau khi Linux nghiệm thu đạt mới chuẩn bị commit được duyệt, build/hash lại nếu cần và trình đúng byte cuối cho chủ repository xác nhận.

`ONE_LINE_AFTER_APPROVAL.txt` trong candidate chứa lệnh một dòng dự kiến: tải bootstrap cố định qua HTTPS → kiểm SHA bootstrap đã ghim → downloader kiểm SHA archive trước tar → `--install` mở đúng UI. **URL chưa được phát hành và lệnh chưa được chạy.** Chỉ dùng sau khi được duyệt/publish đúng các byte đó; không curl|bash bỏ kiểm checksum, không đưa token vào lệnh.
