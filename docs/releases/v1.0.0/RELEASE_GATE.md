# v1.0.0 — điều kiện cho phép push và phát hành

Repository đích: https://github.com/Luanhoangkaki/ZIC-Xray-Agent

Hồ sơ cập nhật 2026-11-01: chỉ chuẩn bị GitHub **Pre-release TEST ONLY**, tag `v1.0.0-audit2-test`. Không tạo tag chính thức `v1.0.0`. Candidate cuối ở `dist/release-candidates/v1.0.0-audit2-test/`; bộ `audit2` cũ được giữ nguyên để đối chiếu. Kết quả mới phải đọc từ TEST_EXECUTION.json/PACKAGE_CHECK.json của candidate cuối; số đếm trong báo cáo audit cũ là lịch sử. Tất cả mục Linux/systemd/VPS/privacy/soak chưa thực thi tiếp tục HOLD. Source commit thực, đối chiếu artifact sau commit và chủ repository duyệt đúng source/asset vẫn HOLD; manifest không thay commit SHA.

Trạng thái: **HOLD — chưa được phép push, tạo tag, tạo GitHub draft/release hoặc upload asset.** Chỉ chuẩn bị file cục bộ. Theo yêu cầu của chủ repository, phải có kết quả kiểm thử thực tế đạt và xác nhận rõ ràng của chủ repository trước mọi thao tác đó. Không coi yêu cầu chuẩn bị là xác nhận phát hành.

## Installer và phạm vi hỗ trợ

Giữ đúng bốn bước nhập, không thêm câu hỏi:

1. Chọn số từ menu Shadowsocks / Trojan / Hysteria / VLESS rồi Enter.
2. Tên miền Panel rồi Enter.
3. API Key hiển thị khi nhập rồi Enter.
4. Node ID rồi Enter.

Sau khi xác thực API, node, cấu hình, policy và tiền kiểm core thành công, installer mới commit credentials, cài Agent và khởi động systemd. Thất bại phải rollback an toàn. Giữ observe, không POST traffic, không sửa/start/restart Xray gốc hay tác động Lab hiện tại.

Hiện chỉ có VLESS subset đủ implementation để tiến tới nghiệm thu Linux; **chưa có giao thức nào được thông qua cổng phát hành Debian 12**. Shadowsocks, Trojan, Hysteria1 và Hysteria2 vẫn phải từ chối cài vì adapter chưa hoàn thiện. Menu không phải danh sách cam kết hỗ trợ. Không tự đổi Hysteria thành Hysteria2. Chỉ cập nhật support matrix sau khi implementation và kiểm thử đúng protocol/transport/TLS đã đạt; không suy rộng test VLESS TCP thành toàn bộ VLESS/REALITY.

## Những điều kiện còn chặn

| Điều kiện | Bằng chứng hiện có | Trạng thái |
| --- | --- | --- |
| Go regression và core fixture | Audit2 Windows: 73 test cấp cao PASS, 1 Linux-only SKIP | Đã có kết quả cục bộ; không thay Linux |
| Build amd64/arm64 | 6 tool binary + 22 test binary | Build PASS; Linux execution NOT RUN |
| Test Linux trên cả hai kiến trúc | Chưa có kết quả thực thi | CHẶN |
| Debian 12 systemd/visible TTY/install/restart/reboot/readiness/PID/permissions | Chỉ mock Git Bash PASS | CHẶN |
| Update/rollback thực tế, giữ state và không replay UNKNOWN | Chỉ regression/mock | CHẶN |
| VPS 1 vCPU / 1 GB: Agent/Xray/system CPU/RSS, FD, I/O và soak 10/20/30 kết nối; latency/jitter/throughput trước/sau | Chỉ microbenchmark/test process Windows | CHẶN |
| Privacy trên host thực tế: stdout/stderr/journal/core/temp/receipt | Source fixes + canary tests; chưa kiểm host | CHẶN |
| Source/artifact khớp audit fixes | Candidate mới tại `dist/release-candidates/v1.0.0-audit2/`, tạo từ source/build mới, giữ gói cũ riêng | Xem kết quả kiểm manifest/binary trong candidate; chưa phải nghiệm thu Linux |
| Quét secrets source và từng archive cuối cùng | Quy trình candidate quét source/evidence và archive giải nén | Xem SECURITY_SCAN và SOURCE_SECURITY_SCAN của đúng candidate; phải quét lại nếu thay file |
| Chủ repository xác nhận đúng commit và asset hashes cuối | Chưa yêu cầu/chưa nhận xác nhận phát hành | CHẶN |

Chạy tiếp chỉ trên Debian riêng không có dữ liệu khách hàng/dịch vụ đang hoạt động theo [runbook đo và kiểm privacy](../../audits/debian12-privacy-measurement.md). Không triển khai production hoặc dùng active Lab để lấp mục NOT RUN. Không yêu cầu chủ repository xác nhận phát hành khi bộ file cuối và kết quả còn thiếu.

## Bộ file cuối cần chuẩn bị sau khi qua kiểm thử

- Source được review, chỉ stage theo allowlist; kiểm diff Git và từng file, không dùng `git add .` để lấy cả workspace. Workspace hiện có commit nền `428a0fa`; kiểm lại đầy đủ SHA của commit thực tế khi chuẩn bị phát hành, không suy ra commit phát hành từ hash manifest.
- `zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz` và `.tar.gz.sha256`.
- `zic-xray-agent-installer-debian12-arm64-v1.0.0.tar.gz` và `.tar.gz.sha256`.
- `SHA256SUMS` của asset cuối; source/build manifest gắn với đúng commit và evidence đã làm sạch.
- `get-zic-xray-agent-v1.0.0.sh` cùng `.sha256`: ghim repository/tag và SHA hai archive **mới**, kiểm trước mọi tar/execution. Một lệnh có `--install` khởi chạy installer đã xác minh; không lấy `latest`, không hardcode credential. Chỉ công bố lệnh tải sau khi có URL/hash cuối được duyệt.
- Release notes/support matrix và hướng dẫn cài/update/rollback, nêu chính xác phạm vi observe, các giao thức bị từ chối và kết quả NOT RUN còn lại. Không gắn production-ready khi chưa nghiệm thu thực tế.

Không upload `.tools`, `.cache`, `panel-reference`, `.git`/SSH keys, config/state/ledger, receipts, backup, cert/private keys, test runtime output hoặc dữ liệu khách hàng. Test source với fixture giả phải được review như source, không trộn test executable/fixture runtime vào installer. `.gitignore` chỉ là lớp hỗ trợ, không thay secret scan.

## Bảo toàn gói cũ và điểm xác nhận cuối

`dist/release/v1.0.0/` là snapshot review cũ, giữ nguyên byte/checksum; các file READY_FOR_REVIEW trong đó **không cấp quyền phát hành và không chứng minh audit fixes đã được đóng gói**. [Báo cáo audit](../../audits/v1.0.0-privacy-resources.md) và `dist/audit-v1.0.0/RELEASE_HOLD.txt` mô tả giới hạn. Hồ sơ này bổ sung yêu cầu nghiệm thu chặt hơn checklist cũ.

Được phép chuẩn bị candidate ở thư mục riêng để kiểm thử trước khi nghiệm thu Linux; không đồng nghĩa được phát hành. Bộ `audit2` dùng API Key hiển thị lúc nhập theo yêu cầu mới, không in lại key trong chương trình/log. Sau khi mọi điều kiện kỹ thuật đạt: quét/checksum/test downloader lại, cung cấp commit/diff, support matrix, tên asset, SHA256 và toàn bộ kết quả để chủ repository review. Chỉ khi nhận xác nhận cụ thể cho bộ đó mới thực hiện push/tag/Release. Nếu source hoặc asset thay đổi sau xác nhận, phải kiểm lại và xin xác nhận cho bộ mới trước khi ghi lên GitHub. Không có thao tác GitHub hoặc thay dịch vụ nào được thực hiện khi viết hồ sơ này.
