# Đo tiếp trên Debian 12 riêng — chưa thực thi

Chỉ dùng VM/VPS bỏ được, 1 vCPU / 1 GB, không có Xray/Agent/Lab đang hoạt động hay dữ liệu khách hàng. Không dùng production hoặc Lab hiện có. Tất cả credential/UUID/cert tự sinh; firewall ngoài VM chỉ cho loopback và chặn outbound không cần thiết. Không sử dụng một panel thật để thử auth hoặc POST.

## Chạy test Linux an toàn trước

Copy source đã review, `dist/linux-<arch>/`, `dist/lab-tests/linux-<arch>/`, `dist/SHA256SUMS` và official Xray cùng kiến trúc vào VM riêng; đối chiếu manifest từ kênh tin cậy. Ví dụ dưới giả định source nằm `/opt/zic-review`, official Xray đã kiểm checksum tại `/opt/zic-review-core/xray`; không tải hoặc restart core đang có.

```bash
cd /opt/zic-review
umask 077
export XRAY_BINARY=/opt/zic-review-core/xray
ZIC_LAB=1 bash scripts/test-debian12.sh
bash scripts/test-installer.sh
bash scripts/test-lab-startup.sh
```

Runner hiện có không cài service; vẫn chỉ chạy trên VM riêng. Linux test `/proc` và permission test không được SKIP rồi gắn PASS. Với mỗi kiến trúc phải chạy chính 11 test binary tương ứng; build chéo không thay thế thực thi. Chạy `go test -race ./...` trên máy Linux kiểm thử có toolchain/CGO phù hợp, không cài toolchain lên VPS đang vận hành. Kiểm unit bằng systemd-analyze verify trước khi xét cài test service.

Để nghiệm thu systemd/reboot cần một Lab **mới** trên VM riêng theo `docs/debian12-lab.md`, build/package từ source audit đã review. Các lệnh setup/service/reboot trong tài liệu đó chưa được chạy trong đợt audit này và không dành cho Lab hiện có. Không tái sử dụng gói Release cũ vốn chưa chứa audit fixes.

## Đo aggregate CPU/RSS/I/O/FD, không lấy thông tin khách hàng

Sau khi người kiểm thử tự tạo Lab mới và xác nhận observe, lấy MainPID bằng systemd (chỉ PID số). Dùng collector mới đã kiểm checksum:

```bash
cd /opt/zic-review
umask 077
case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) exit 1;; esac
chmod 0700 "./dist/linux-$arch/zic-bench"
agent_pid=$(systemctl show -p MainPID --value zic-lab-agent.service)
core_pid=$(systemctl show -p MainPID --value zic-lab-core.service)
test "$agent_pid" -gt 0 && test "$core_pid" -gt 0 || exit 1
sudo install -d -m 0700 -o "$(id -u)" -g "$(id -g)" /var/lib/zic-review-evidence
sudo "./dist/linux-$arch/zic-bench" \
  -agent-pid "$agent_pid" -xray-pid "$core_pid" \
  -io-fd -duration 30m -interval 5s \
  > /var/lib/zic-review-evidence/idle-resources.csv
```

Lệnh tạo evidence directory ở trên cấp owner cho tester để shell redirect được; `sudo` cho binary không tự cấp quyền cho shell redirect. Không cấp API Key qua argv/env. CSV có timestamp **lấy mẫu hệ thống**, CPU/RSS aggregate và count FD, không có UUID/IP/URL/connection event. Mặc định không có flag `-io-fd` vẫn giữ schema cũ. Collector tự ghi CPU/RSS/I/O/FD của chính mình riêng; không cộng overhead đó vào Agent/Xray.

`kernel_write_bytes_total/delta` là Linux task I/O accounting, **không phải physical SSD/NAND writes hoặc write amplification**. Ghi riêng delta block-device sectors trong `/proc/diskstats` trên ổ VM dành riêng, đối chiếu nền idle/host (và backing device/hypervisor nếu cần số SSD thật). Không đọc filenames, FD symlink targets, `/proc/PID/environ`, heap/stack dumps hoặc packet capture để làm evidence. Chưa có goroutine live metric cho agent/core: ghi **NOT AVAILABLE**, không suy ra từ số thread hoặc FD.

Collector dừng khi PID/starttime đổi. Tạo file CSV/phase mới sau restart/reboot và xác nhận PID mới; không nối counter của hai process làm một. Trường write_bytes không thay thế fsync/durability validation. Giới hạn tối đa 24h/collector run; interval tối thiểu 1s.

## Thiết kế phép so sánh và fault test

1. Dùng cùng loại VPS, kernel, CPU quota, bộ nhớ/swap, phiên bản core, TLS/REALITY, dữ liệu giả, workload và interval cho bản trước/sau. Lưu SHA256 của hai binary. Không giảm encryption, tắt kiểm chứng chứng chỉ, thay transport hoặc hạ resource cap để cải thiện kết quả.
2. Warmup 5 phút, idle 30 phút, rồi từng pha 10/20/30 kết nối có mức gửi cố định trong ít nhất 30 phút; lặp cùng thứ tự và tải. Collector đo Agent/Xray/system, load generator trên máy khác hoặc đo riêng bằng `-load-pid`. Loopback echo hiện có chỉ là functional accounting workload, không đại diện Internet/REALITY performance.
3. Thực hiện soak ít nhất 24h cho mỗi bản với tải định trước; theo dõi RSS/CPU p50/p95/peak, slope FD, I/O delta, lỗi kỹ thuật cố định và accounting. Không kết luận memory leak từ một lần GC hoặc 300 request test.
4. Đo latency p50/p95/p99, jitter, throughput, lỗi/tái kết nối bằng endpoint **do tester sở hữu**, IP/UUID chỉ giữ trong config private; báo cáo aggregate, không access logs. Không gửi traffic hoặc báo cáo POST về production.
5. Restart riêng Agent, restart riêng core và reboot VM thử. Đối soát bytes theo UUID giả trước/sau, epoch/PID mới, readiness và file `/run/zic-lab-core/xray.pid` tái tạo; khôi phục authorization/quota không bị nới. Fault trước/sau fsync/rename cần VM riêng, không fault-inject ổ của Lab đang chạy.
6. Kiểm rollback transaction fresh install, lỗi startup/readiness, file sửa sau cài và drop-in mới. Không rollback ledger hoặc tự retry UNKNOWN. Kiểm không còn process/FD/temporary state orphan sau nhiều fault; ghi riêng số lượng và tổng byte, không nội dung.

## Kiểm privacy trên host riêng

- Xác nhận units mới có DynamicUser/LoadCredential/sandbox/resource caps và stderr/stdout null **sau khi cài vào VM riêng**. Kiểm core configuration mới tắt access/error/DNS logs. Không sửa core gốc hoặc active Lab để thử.
- Chỉ dùng canary giả để gây lỗi API/timeout/TLS/JSON/credential/readiness/panic; scanner đọc stdout/stderr/journal/log/temp trên VM và chỉ xuất PASS/FAIL + loại lỗi cố định, không in matched value. Không copy journal thô vào báo cáo chia sẻ. Journal có thể vẫn có service status/technical timestamps; không được có dữ liệu khách hàng/canary.
- Review kernel `core_pattern`, host coredump collector, swap/hibernation, backups và quyền state/receipt. Không coi `LimitCORE=0` là đủ khi kernel pipe dump; chưa có global host policy được áp dụng. Tránh tạo actual core/heap/stack dump chứa dữ liệu, dù là trong phép thử.
- Kiểm file mode/directory metadata mà không `cat` config, ledger, môi trường hoặc credential. Installer và rollback giữ credentials để recovery; private state không được đưa vào log/evidence công khai.
- Ghi mọi mục chưa chạy là NOT RUN. Chỉ chấp thuận sau khi có evidence Linux thật, bao gồm các phép đo so sánh; chưa phát hành hay dùng nhãn production-ready.

## Candidate audit2 và kiểm TTY

Candidate mới nằm trong `dist/release-candidates/v1.0.0-audit2/`. Không dùng archive review cũ. Kiểm SHA256SUMS của candidate trước khi giải nén hoặc dùng binary. Source archive chỉ chứa source đã allowlist, không chứa `dist`, official core hay runtime config. Chuyển riêng `dist/linux-<arch>`, `dist/lab-tests/linux-<arch>` và `dist/SHA256SUMS` từ bộ build mới nếu chạy test bằng binary. Không cần cài compiler lên VPS 1 GB.

Trên VM riêng, xác nhận đủ 4 prompt theo đúng thứ tự và API Key hiển thị khi gõ; chương trình không in lại key sau Enter, kể cả khi auth/TLS/node sai. Chỉ dùng token giả. Không bật shell xtrace, session recorder, terminal transcript hoặc screen capture cho bước nhập có chủ đích hiển thị key. Kiểm app stdout/stderr và journal riêng với canary chỉ trong RAM; không đưa key vào evidence. Kiểm file 0700/0600 và credential qua LoadCredential mà không in nội dung. Mock hiện chỉ kiểm cú pháp normal read và không lộ key ở output/argv/environment, không chứng minh echo TTY thật.
