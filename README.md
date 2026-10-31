# ZIC-Xray-Agent

Go agent cho Debian/Ubuntu amd64/arm64, mục tiêu VPS 1 vCPU / 1 GB RAM. **Observe là mặc định. Không có công tắc gửi traffic thật, không có production manage mode.** Mã đã có chế độ thử `manage-test` trên loopback và hàng đợi traffic bền vững có trạng thái UNKNOWN.

## Trạng thái hiện tại

| Phần | Đã triển khai | Phạm vi kiểm chứng |
| --- | --- | --- |
| Panel | Adapter UniProxy V1 theo source V2B-ZIC production đã pin, config/UUID/ETag | Source đã đọc; HTTP test dùng mock TLS, không chạy panel production |
| Daemon | Một worker/timer, lịch Stats độc lập backoff panel, SIGTERM, khóa OS, snapshot/checksum và queue atomic | Unit test, crash tiến trình thật và daemon nối mock → core thật trên Windows |
| VLESS/REALITY | Controller sở hữu inbound riêng, intent ghi trước mutation, inspect core để tiếp tục sau lỗi/crash, đổi transport/UUID, thu hồi user | TCP/REALITY primitive và lifecycle qua official core; handshake REALITY đầy đủ chưa thử |
| Authorization | Danh sách panel là nguồn quyền; user biến mất được thu hồi, UUID cũ bị loại; cache stale/future bị từ chối | Không suy ra expiry/quota chưa được API cung cấp; speed/device limit dương làm tắt inbound thử, không bỏ qua |
| Traffic | Counter reset=false, baseline/delta/queue cùng checkpoint, UNKNOWN khi mất kết quả, manual status/export/resolve | Hai user 10/20/30 kết nối; crash trước/sau POST dummy, timeout/500, restart không replay |
| Systemd/installer | Menu 1–4, API Key nhập ẩn, tiền kiểm panel/core, tự khởi động observe và rollback; DynamicUser/LoadCredential, tài nguyên/log giới hạn | Go/native core + rootless installer mock; runtime installer/systemd Debian chưa kiểm |
| Debian 12 | Test suite dựng sẵn amd64/arm64, runner chỉ chạy trên VM thử nghiệm trống | Đủ chuẩn bị cho lần kiểm thử cách ly đầu tiên; chưa có kết quả Linux thực tế |
| RAM/CPU | Một binary agent, không nhúng core, không có Python/Node/Java/PHP daemon; log 4 MiB | Chưa đo VPS: <20 MB RSS và <1% CPU vẫn là mục tiêu |

Xem [biên bản](docs/verification.md), [chính sách dữ liệu](docs/traffic-policy.md) và [runbook Debian 12](docs/debian12-lab.md). Không coi unit test/build là bằng chứng production-ready.

## Source panel chỉ đọc

Reference nằm trong `panel-reference/`, repository [kutycma/v2b-zic](https://github.com/kutycma/v2b-zic), production/default master `f778400907da4c47f0466abcab21030009e2770e`. Nhánh dev có commit khác, bốn file API được yêu cầu không khác production ở lần clone. [Audit](docs/panel-api.md) lưu bằng chứng và hash checkout.

Không sửa source, API, database, Redis, queue, không chạy PHP panel. API không trả protocol trong config VLESS, auth/node lỗi có thể là HTTP 500; push dùng `{"user_id":[upload,download]}` và phản hồi `{"data":true}`. Agent không nhân rate. Không suy rộng sang panel khác.

## Build và test

Go 1.26+, dependencies gRPC/protobuf đã pin; không import module Xray-core. Xray chính thức là tiến trình riêng do operator quản lý.

```powershell
go test ./...
go vet ./...
powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/build.ps1 -IncludeTests
```

Linux build host dùng `bash scripts/build.sh --include-tests`. Không cần cài Go trên VPS nếu chuyển binary/test binary đã dựng. `dist/SHA256SUMS` bao gồm cả agent, công cụ đo riêng và test suite. Checksum kiểm tra byte, không thay thế xác minh nguồn phát hành.

Bộ thử nghiệm standalone Debian 12 amd64: [hướng dẫn cài/test cách ly](docs/debian12-lab.md). Có `lab.sh`, core chính thức riêng, panel/UUID giả, 11 suite amd64 và đo CPU/RSS, 10/20/30 kết nối, Stats/ledger từng UUID cùng recovery. Không gửi traffic thật, không sửa Xray hiện hữu. Build cả hai kiến trúc có 22 test binary; Linux/VPS chưa được chạy đo trên host Windows.

`XRAY_BINARY` trong test trỏ đến core chính thức đã xác minh. Test khởi động một core tạm trên loopback với dữ liệu giả, không sửa cấu hình core hiện hữu. Nếu không đặt biến, bài core thật SKIP. Installer cũng cho phép biến này để chọn binary tiền kiểm offline; mặc định dùng core đóng gói chỉ chạy `-test`.

## Cài observer: chỉ sau phê duyệt máy thử nghiệm

Giải nén gói installer Debian 12 đúng kiến trúc rồi chạy `sudo bash ./install.sh`. Có đúng menu Shadowsocks/Trojan/Hysteria/VLESS, sau đó Panel domain → API Key nhập ẩn → Node ID; không có câu hỏi khác. Bản này chỉ cài VLESS đã qua tiền kiểm; các adapter chưa có bị từ chối rõ ràng. Phân biệt Hysteria1/2 bằng version panel, không tự đổi giao thức. Xem [hướng dẫn installer và bộ test](docs/interactive-installer.md).

Config nguồn root 0600 trong thư mục 0700; token qua stdin, không argv/env/log. Snapshot panel có thể chứa private key và cũng phải bảo vệ. Bộ cài từ chối bản cũ, xác minh API/policy và core chính thức bằng config riêng với `-test`, rồi enable/start duy nhất zic-xray-agent.service ở observe, đợi snapshot xác thực. Lỗi thì rollback file/service mới, giữ evidence; không sửa hoặc khởi động server Xray, không thay Lab đang chạy và không triển khai VPS từ máy phát triển.

DynamicUser/LoadCredential giới hạn quyền; state/log ở `/var/lib/zic-xray-agent/data`, `/var/log/zic-xray-agent/files`. Thư mục con thực tương thích symlink thư mục cha của systemd. Log app tối đa 4 x 1 MiB; journal stderr vẫn theo retention hiện có, không đổi journald toàn máy. Các giới hạn CPU/memory là ngân sách thử nghiệm, chưa chứng minh mức sử dụng.

## Chế độ và quản lý core

- `observe` mặc định: đọc/lưu config/user; nếu cấu hình Stats thì thu counter và gộp batch PENDING. Không mutation, không POST.
- `manage-test` phải được ghi rõ trong config riêng: chỉ panel HTTPS với **IP loopback**, listen IP loopback, API/PID core rõ ràng. Đích REALITY cũng phải loopback. Chỉ dùng tài khoản giả trên môi trường được duyệt; flag không thay thế trách nhiệm chọn đúng môi trường.
- `manage` bị từ chối. `traffic_send_enabled=true` bị từ chối ở mọi mode. Không có lệnh `traffic send/retry`.
- Sender thư viện chỉ dùng qua test opt-in, từ chối panel ngoài loopback; daemon không gọi sender, kể cả manage-test.

Controller chỉ chạm tag riêng `zic-node-<id>` có manifest/intent chứng minh sở hữu. Không tiếp quản tag lạ hoặc config drift. Không rollback về UUID đã bị thu hồi; nếu thay config gián đoạn, lần sau inspect state core và tiếp tục theo snapshot mới đã được panel xác nhận. Core reboot sẽ khôi phục inbound sau lần kéo panel thành công, không cấp quyền từ cache chưa tái xác nhận.

Khi panel offline, quyền cũ có lease mặc định 120 giây, tối đa cấu hình 3600. Đến hạn, controller thử tắt inbound thuộc quyền sở hữu. API lỗi có thể ngăn thao tác này; RemoveUser/listener không chứng minh mọi session cũ đã dừng. Không dùng chế độ này cho khách hàng thật.

## Stats và đối soát

Stats chỉ bật khi thêm `xray_api_endpoint` (IP loopback:port) và `xray_pid_file` đáng tin, đúng core. Agent không tự tạo PID file bằng cách sửa unit Xray. Linux epoch là boot ID/PID/starttime, kiểm tra trước/sau RPC.

Ledger v2 giữ baseline, pending, queue và incident trong một checkpoint checksum/fsync/rename; v1 được đọc và bảo toàn pending khi ghi nâng cấp. Lần mẫu đầu chỉ lập baseline. Đọc lại counter hoặc restart cùng core không cộng lại byte đã ghi. Core epoch mới giữ traffic cũ đã biết, lấy counter mới từ zero, ghi incident cho tail cũ chưa biết. Counter giảm/mất dừng thu và giữ evidence; không tự reset mốc để giấu sai lệch.

Batch chưa từng gửi được gộp có giới hạn; từ SENDING payload đóng băng. Restart gặp SENDING chuyển UNKNOWN; UNKNOWN/incident mở chặn sender. ACCEPTED chỉ là HTTP data=true, không chứng minh job/billing bền vững. Chi tiết giới hạn ở [traffic-policy.md](docs/traffic-policy.md).

```bash
zic-xray-agent traffic status --state-dir /var/lib/zic-xray-agent/data
```

Status chỉ trả metadata, không gửi mạng hay sửa trạng thái. Các lệnh đối soát sửa state yêu cầu agent dừng để lấy cùng OS lock:

```bash
zic-xray-agent traffic export --state-dir /path/to/test-state --id BATCH_ID --digest PAYLOAD_SHA256
zic-xray-agent traffic resolve --state-dir /path/to/test-state --id BATCH_ID --digest PAYLOAD_SHA256 --decision applied --evidence TICKET_ID
zic-xray-agent traffic acknowledge-gap --state-dir /path/to/test-state --id INCIDENT_ID --evidence TICKET_ID
```

`not-applied` là quyết định đối soát khác, không tự requeue/resend. Export chỉ cho batch đã thử, nằm trong thư mục exports 0700/file 0600; không tự POST file export. Không ghi token/dữ liệu khách hàng vào evidence. Acknowledge-gap chỉ ghi nhận rủi ro, không tạo thêm byte đã mất.

Không có bảo đảm không mất data tuyệt đối. Chưa thử Linux/systemd/reboot/power-loss thật, chưa đo VPS 1 GB; mọi triển khai hay thay đổi Xray hiện hữu vẫn cần phê duyệt.
