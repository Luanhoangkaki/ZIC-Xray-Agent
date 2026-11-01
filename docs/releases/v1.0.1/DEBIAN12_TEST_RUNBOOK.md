# Debian 12: nghiệm thu riêng cho v1.0.0-audit2-test

Trạng thái ban đầu: **NOT RUN** cho toàn bộ các bước host dưới đây. Đây là quy trình chuẩn bị; không có quyền triển khai lên VPS hay thay Lab/production. Chỉ người vận hành đã được phép mới chạy trên VPS Debian 12 mới, 1 vCPU / 1 GB RAM, không dữ liệu khách hàng, snapshot riêng để phục hồi. Làm riêng AMD64 và ARM64; ghi architecture, kernel, quota, RAM/swap và SHA binary vào evidence private. Không dùng installer `20261031-r1`.

## 1. Chuẩn bị và kiểm nguồn

- Dùng candidate `dist/release-candidates/v1.0.0-audit2-test/`. Kiểm SHA bootstrap từ báo cáo được duyệt qua kênh tin cậy, archive trước giải nén và manifest trong gói. SHA tải cùng nguồn không thay thế việc xác thực nguồn.
- Mang riêng source review, binary `zic-bench`/`zic-lab`, 11 test binary đúng kiến trúc và `dist/SHA256SUMS` từ build tương ứng. Chúng không phải installer assets. Không cài compiler để đo RSS trên VPS 1 GB.
- Khi chưa cài service, chạy Linux functional runner theo [runbook đo](../../audits/debian12-privacy-measurement.md). Không đổi SKIP thành PASS; đọc từng test log riêng. Chạy race detector trên build host Linux đủ tài nguyên nếu có toolchain phù hợp.
- Chuẩn bị HTTPS mock panel chỉ GET với schema của contract đã pin, users/UUID giả, token giả và CA được kiểm chứng. Không dùng token/panel production. HTTPS phải được xác thực; không dùng `curl -k` hoặc bỏ TLS verification. Không in token từ manifest ra terminal để sao chép vào báo cáo.
- Mock dùng cho thử installer phải sống qua reboot và sẵn sàng trước lần khởi động agent. Nếu dùng private CA thì cần cấu hình trust cho cả preflight và namespace LoadCredential của service trên **máy test riêng**, ghi thay đổi này vào evidence. Installer đóng gói không tự cài CA. Không chạy các service mang tên active Lab vì downloader cố ý từ chối chúng; dùng mock độc lập do tester sở hữu. Không vô hiệu hóa guard để thử trên Lab cũ.
- Dùng panel mock loopback cho VLESS TCP không TLS; đó chỉ là functional fixture. Kiểm REALITY/TLS thực cần endpoint riêng đúng cấu hình. Không suy từ `run -test` thành handshake REALITY đã PASS.

## 2. TTY và cài mới

Mở SSH TTY thật, tắt session recording/transcript/xtrace. Chạy lệnh trong `ONE_LINE_AFTER_APPROVAL.txt` **sau khi đúng assets đã được duyệt và publish**; lệnh tự chọn AMD64/ARM64. Nếu chưa publish, chuyển archive qua kênh tin cậy, kiểm checksum và chạy `sudo bash ./install.sh` trong thư mục đã xác minh.

Xác nhận đúng bốn prompt protocol → Panel domain → API Key Panel → Node ID; API Key hiển thị khi gõ theo yêu cầu. Không có prompt nghiệp vụ thứ năm. `sudo` có thể hỏi mật khẩu hệ điều hành, độc lập với UI installer. Chỉ dùng token giả không đưa vào argv/env/URL dòng lệnh. Chương trình không in lại key sau Enter hoặc khi lỗi.

Kiểm positive VLESS subset; dùng VM/snapshot sạch riêng cho từng negative: Shadowsocks, Trojan, Hysteria1/2; token sai; node sai; TLS/JSON/policy sai; core sai SHA; readonly/full disk; service start/readiness thất bại. Unsupported adapter phải dừng trước commit credentials/service. Mọi lỗi phải dùng thông báo cố định, không raw URL/body/user. Không làm đầy ổ đang giữ evidence/accounting; fault storage dùng volume/VM dùng một lần.

## 3. systemd, PID, readiness và quyền

Các lệnh sau chỉ dành máy test vừa cài, chưa chạy trong audit Windows:

```bash
sudo systemd-analyze verify /etc/systemd/system/zic-xray-agent.service
sudo systemctl is-enabled zic-xray-agent.service
sudo systemctl is-active zic-xray-agent.service
sudo systemctl show zic-xray-agent.service -p MainPID -p NRestarts -p ActiveState -p SubState
sudo /usr/local/lib/zic-xray-agent/zic-xray-agent install-status --config /etc/zic-xray-agent/config.json --state-dir /var/lib/zic-xray-agent/data
sudo stat -c '%a' /etc/zic-xray-agent /etc/zic-xray-agent/config.json /etc/zic-xray-agent/panel-config.json
```

Kỳ vọng config dir 0700, config files 0600; kiểm tương tự state/log/receipt và namespace private của DynamicUser mà không `cat` nội dung. Xác nhận LoadCredential hoạt động, chỉ một agent process, không proxy listener và không traffic POST. PID phải là process của unit, không tái dùng PID cũ; kiểm đồng bộ identity/freshness bằng `install-status`, không coi active đơn thuần là ready.

Thử `stop` → `start`, rồi `restart`; ghi PID/NRestarts và readiness sau mỗi lần. `systemctl enable --now` do installer thực hiện phải thành công; reboot máy test một lần, đợi network/mock sẵn sàng, kiểm enabled/active/MainPID mới và snapshot fresh. Chỉ `sudo reboot` sau khi tester đã có console khôi phục; không áp dụng cho Lab/production. Kiểm các policy/auth/expiry/quota không bị nới sau recovery.

## 4. Rollback và gỡ cài đặt

Theo [UPDATE_ROLLBACK.md](UPDATE_ROLLBACK.md), dùng **đúng transaction** installer in ra. Không tự chọn receipt bằng wildcard và không xóa file để làm fresh-install guard PASS.

- Fault ở preflight, publish, unit verify, enable/start và readiness: agent không được để active khi đã báo rollback thành công. SIGKILL/mất điện có thể cần review thủ công; chưa bảo đảm transaction toàn filesystem.
- Manual rollback của fresh install là đường gỡ service/binary/config do transaction sở hữu. Config chuyển vào `removed/config` private; state/ledger/log vẫn giữ để recovery. Không gọi đây là secure erase.
- Sau rollback, kiểm disabled/inactive, không còn process thuộc unit; quyền config retained 0700/0600; Xray/mock và các service ngoài transaction không đổi. Kiểm rollback lặp lại không phá dữ liệu.
- File hoặc drop-in sửa sau cài phải làm rollback từ chối trước thay service. Manual update/rollback chỉ đổi binary/unit sau review tương thích state; không phục hồi ledger cũ, không replay UNKNOWN. Dùng VM mới cho lượt cài kế tiếp.

## 5. Privacy, tài nguyên, mạng và soak

Theo [quy trình privacy và đo](../../audits/debian12-privacy-measurement.md), scanner trên máy test đọc canary giả trong RAM, kiểm stdout/stderr, rotated logs, journal, gRPC/panic, temp, receipt và diagnostics; chỉ xuất PASS/FAIL/count, không matched value. Credential/snapshot/ledger là state nhạy cảm cần thiết, khác log truy cập. Không xuất chúng hoặc journal thô ra báo cáo. Kiểm host core_pattern/coredump collector, swap, backup và lịch sử log; `LimitCORE=0` không chứng minh mọi coredump đã bị chặn.

Đo riêng Agent, official Xray, system, collector và load generator: idle 30 phút sau warmup 5 phút; từng pha 10/20/30 kết nối ít nhất 30 phút; soak ít nhất 24 giờ cho mỗi bản so sánh. Workload, encryption/TLS, transport, interval, core, VPS/kernel và rate phải giống trước/sau. Thu RSS/CPU p50/p95/peak, FD slope, write_bytes và block-device I/O, không FD targets hoặc packet capture. Đo ping/latency/jitter/throughput với endpoint tester sở hữu; báo cáo aggregate. Collector dừng nếu PID/starttime đổi; tạo phase mới sau restart/reboot.

Observer cài bằng release **không cấp user hay bật proxy**. Phép đo 10/20/30 qua Xray cần fixture mới riêng được tester cho phép theo `docs/debian12-lab.md`, không biến observer thành node production. Đo observe trước, rồi opt-in manage-test riêng nếu cần kiểm control/recovery; không bật real sender. Đối soát Stats API với counter không reset; ambiguous send phải durable UNKNOWN, chặn tự retry, không nhận `data=true` là bằng chứng billing bền vững.

Goroutine live của Agent/Xray hiện **NOT AVAILABLE**; số goroutine trong test Windows không thay metric live. Không dùng public pprof hoặc heap/stack dump chứa secret. Mục tiêu RSS Agent <20 MB, idle CPU <1% chưa được đo. Theo dõi `.checkpoint-*` sau crash và receipts qua nhiều lượt; chỉ count/bytes, không xóa state/orphan trước review. Retention/orphan growth và transient queue marshal memory tiếp tục HOLD cho tới khi có bằng chứng thực tế và quy trình an toàn.

## 6. Ghi kết quả

Mỗi bước ghi PASS / FAIL / SKIP / NOT RUN, architecture, hash candidate và loại evidence. Giữ evidence raw trên host private; chia sẻ bảng aggregate đã quét. Lỗi hoặc thiếu metric giữ RELEASE_GATE HOLD. Không sửa nhãn NOT RUN của candidate Windows bằng suy luận từ mock/cross-build. Không tuyên bố không leak, không log nhạy cảm toàn VPS, lossless tuyệt đối hoặc production-ready khi chưa có chứng cứ tương ứng.
