# Bộ thử nghiệm cách ly Debian 12 amd64

**Lỗi startup của archive 20261031 trên systemd Debian 12:** readiness trong ExecStartPost có thể không thấy credentials; bỏ post-start cũng bỏ ghi PID. Với lab đã cài đang dùng workaround, áp dụng [bản vá startup và hướng dẫn rollback](lab-startup-fix.md). Archive cũ được giữ nguyên để đối chiếu; source installer hiện cài thêm start.sh và dùng Type=notify. Không cài đè bằng archive cũ để xử lý lỗi này.

Bản `20261031`: chỉ chuẩn bị artifact; **chưa chạy test binary Linux, systemd hoặc đo trên VPS**. Không production-ready, không cam kết RSS <20 MB, CPU idle <1% hoặc không mất traffic. Các chỉ số đó phải đo thực tế trên VPS Debian 12, 1 vCPU / 1 GB RAM.

## File phát hành

- `zic-xray-agent-lab-debian12-amd64-20261031.tar.gz`
- `zic-xray-agent-lab-debian12-amd64-20261031.tar.gz.sha256`

Archive có `bin/zic-xray-agent`, `bin/zic-bench`, `bin/zic-lab`, `bin/xray`, 11 test binary amd64, ba unit `zic-lab-*`, `lab.sh`, `BUILD.json`, `SHA256SUMS`, giấy phép và evidence build. Xray là binary chính thức **v26.10.29**, không được nhúng vào agent. Archive upstream `Xray-linux-64.zip` đã đối chiếu SHA256 metadata GitHub: `8a5f3e6a1057e4bd0f8a7c57c9d91204b4be0b74222a6e210d070603101163c2`.

Tổng bộ build ngoài archive có **22 test binary**, 11 amd64 và 11 arm64 tại `dist/lab-tests/`. Audit kiểm tra hash và ELF machine (62/183). Compile/audit không phải chạy test Linux. `evidence/windows-tests.log` là kết quả test native Windows với official Windows core; `artifact-audit.json` ghi rõ Linux NOT RUN. Không chạy binary arm64 trên amd64.

## Cách ly

Dùng VPS lab riêng, không khách hàng thật. Installer chỉ tạo `/opt/zic-xray-lab`, state `/var/lib/zic-lab-agent/data`, log `/var/log/zic-lab-agent/files`, runtime `/run/zic-lab-core` và ba unit:

- `zic-lab-panel.service`: mock HTTPS và echo loopback, từ chối mọi POST.
- `zic-lab-core.service`: core chính thức riêng, cấu hình mới riêng và hai client giả trong cùng core.
- `zic-lab-agent.service`: một process agent, **observe**, không POST/không điều khiển core.

Không cài `install.sh` của observer thông thường cho bài này. Không nhập Panel URL/token/Node ID thật. Lab tự sinh token, chứng chỉ và hai UUID giả; Node ID giả cố định chỉ dùng trong fixture. Config 0700/file 0600; credential cấp cho service bằng systemd LoadCredential. Không sửa panel, API, DB, Redis hoặc queue; không mở firewall; không tải gì từ mạng lúc install/test.

Sáu cổng TCP `127.0.0.1:38080–38085`: mock, API Stats, echo, hai SOCKS client, VLESS server. Đổi cụm cổng bằng `install --base-port 39080` nếu bận; không stop dịch vụ chiếm cổng. Core chỉ cho phép VLESS tới đúng echo loopback. Unit có IPAddressDeny=any/IPAddressAllow=localhost; kernel/systemd phải hỗ trợ cgroup BPF để lớp lọc này có hiệu lực. Mọi endpoint của lab vẫn được mã hóa cố định là IP loopback, không có chỗ nhập panel ngoài. Không chia sẻ UUID/config cho ứng dụng khác.

Không đọc hay ghi `/etc/xray`, `/usr/local/etc/xray`, credential agent cũ; không gọi start/stop/restart `xray.service` hoặc `zic-xray-agent.service`. Installer enable/start **chỉ ba service lab**; chúng tự chạy lại sau boot. Không chạy tự động trên VPS từ máy build. Reboot là thao tác toàn VPS, vì vậy chỉ thực hiện trên VPS lab riêng khi người vận hành chấp thuận.

## Các lệnh trên Debian 12

Chuyển cả archive và file `.sha256` bằng kênh tin cậy vào cùng thư mục trên VPS Debian 12 **amd64**. Cần bash, tar, coreutils (sha256sum/getconf), procps (free), findutils (find), util-linux (flock), systemd/systemd-analyze; không cần Go, Python, Node, PHP hay Docker.

```bash
sha256sum --check zic-xray-agent-lab-debian12-amd64-20261031.tar.gz.sha256
tar -xzf zic-xray-agent-lab-debian12-amd64-20261031.tar.gz
cd zic-xray-agent-lab-debian12-amd64-20261031
sha256sum --check --strict SHA256SUMS
sudo env ZIC_LAB=1 bash lab.sh install
```

Installer từ chối ghi đè lab cũ/unit cùng tên/state cũ và cổng bận. Giữ nguyên evidence khi lỗi; không xóa ledger để biến FAIL thành PASS. Nếu install dở dang, xem unit lab đã tạo, dùng `sudo systemctl stop zic-lab-agent.service zic-lab-core.service zic-lab-panel.service` để dừng riêng chúng; không chạm Xray hiện hữu.

Chạy smoke (15 giây mỗi mức, không warm-up); chưa thay thế phép đo dài:

```bash
sudo bash /opt/zic-xray-lab/lab.sh test --quick
```

Chạy bộ đo đầy đủ: 11 suite amd64, warm-up idle 5 phút, idle 30 phút, tải 10/20/30 kết nối, **15 phút mỗi mức**; cộng thời gian đợi checkpoint, khoảng 80–90 phút:

```bash
sudo bash /opt/zic-xray-lab/lab.sh test
```

Mỗi pha đọc Xray Stats API **reset=false**, truyền payload echo hai chiều 4096 byte/lần, khoảng 100 ms/lần/kết nối trên hai UUID, so byte ứng dụng ↔ counter Stats VLESS ↔ delta ledger agent. Không so byte TCP/mã hóa trên NIC. Yêu cầu không lỗi kết nối, byte chính xác từng UUID, PID/epoch không đổi trong pha, không xuất hiện incident mới; lỗi thì lệnh exit khác 0 và giữ file kết quả. Core thật có thể được chạy tạm bởi test suite với config/port riêng, cleanup sau test.

Kiểm tra phục hồi khi không có phép đo/tải khác đang chạy:

```bash
sudo bash /opt/zic-xray-lab/lab.sh restart-check
sudo bash /opt/zic-xray-lab/lab.sh core-restart-check
sudo bash /opt/zic-xray-lab/lab.sh test --quick
sudo bash /opt/zic-xray-lab/lab.sh prepare-reboot
```

`restart-check` chỉ restart agent lab: process phải đổi, core/boot/counter và tổng byte đã biết phải giữ nguyên. `core-restart-check` restart core và agent lab: core epoch đổi, tổng byte cũ giữ nguyên và có thêm đúng một incident `core_epoch_changed`. Không tự acknowledge incident.

Lệnh dưới **chỉ chạy khi đồng ý reboot VPS lab riêng**; script prepare-reboot không tự reboot:

```bash
sudo reboot
```

Đăng nhập lại rồi chạy:

```bash
sudo bash /opt/zic-xray-lab/lab.sh verify-reboot
sudo bash /opt/zic-xray-lab/lab.sh test --quick
```

Reboot phải đổi boot ID/core epoch, giữ tổng byte đã biết và thêm đúng một incident. Hai phép recovery core/reboot này kiểm tra giữ dữ liệu đã checkpoint và phát hiện khoảng hở; **không chứng minh không mất tail traffic** trong RAM core. Quick test sau recovery chứng minh đường dữ liệu mới tiếp tục hoạt động, counter không cộng lại dữ liệu cũ. Không có reboot khi đang truyền hoặc power-loss/disk-full tự động trong gói này; xem ma trận nghiệm thu trong repository `docs/acceptance.md` trước khi mở rộng bài lỗi.

Dừng lab, giữ config/state/evidence:

```bash
sudo bash /opt/zic-xray-lab/lab.sh stop
```

## Đọc kết quả

Console in đường dẫn riêng cho mỗi lần chạy. Chỉ root được đọc `/opt/zic-xray-lab/results/`:

- `run.*/environment.txt`, `services-before.txt`, `services-after.txt`: OS/kernel, số CPU, RAM/swap, core version, PID/NRestarts/cgroup. MemoryCurrent/Peak là cgroup memory, không phải RSS.
- `run.*/*.test.log`: PASS/FAIL từng suite Linux, gồm API core thật, crash/UNKNOWN và không replay POST tới mock. Các test sender chỉ POST loopback HTTPS giả, không phải panel thật.
- `run.*/connections-{0,10,20,30}/resources.csv`: timestamp, CPU/RSS riêng **agent**, **Xray**, collector, load controller; CPU busy, memory available/used và swap toàn máy. 100% CPU tiến trình = một vCPU, RSS dùng byte.
- `resources-summary.json`: mean/p95/peak từng chỉ số, lấy mẫu mỗi giây. Mục tiêu agent RSS ổn định <20,000,000 byte và CPU idle trung bình <1% chưa tự đánh giá PASS; kiểm tra dữ liệu thật, throttling/OOM và cảnh báo systemd. GOMAXPROCS/GOMEMLIMIT/MemoryMax/CPUQuota là giới hạn cấu hình, không phải số đo.
- `summary.json`: UUID giả, expected upload/download, Stats delta, số lỗi và passed. `accounting.json`: ledger delta chính xác từng UUID. `ledger-before.json`/`ledger-after.json`: evidence checkpoint.
- `restart.*/recovery.json`, `reboot-recovery.json`: trước/sau và kết quả recovery. `installed.json`, `reboot-before.json`: marker nền.

Đây là tải tổng hợp loopback, **core RSS/CPU bao gồm cả VLESS client và server của fixture**, mock và bộ sinh tải cũng nằm cùng VPS. Có số đo riêng collector/load để thấy overhead, nhưng không phải benchmark server ngoài mạng hoặc REALITY/Vision, không tự trừ overhead khỏi CPU core. Chưa có PSS/cgroup throttling/steal riêng/soak 24h; nếu cần nghiệm thu hiệu năng thực, dùng máy sinh tải ngoài VPS và kế hoạch `docs/acceptance.md`. Không dùng kết quả Windows hoặc quick để khẳng định đạt mục tiêu 1 vCPU/1 GB.

## Tái tạo trên máy build Windows

```powershell
$env:GOCACHE = 'C:/ZIC-Xray-Agent/.cache/go-build'
$env:XRAY_BINARY = 'C:/ZIC-Xray-Agent/.tools/xray/bin/xray.exe'
go test -count=1 -v ./... > dist/verification/windows-tests.log
go vet ./...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -IncludeTests
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/package-lab.ps1 -BundleID 20261031
```

Packager cần metadata core chính thức cùng evidence vet/bash/panel audit trong `dist/verification`, archive upstream đã xác minh. Từ chối ghi đè archive có sẵn; dùng BundleID mới và cập nhật hướng dẫn tên file khi tái phát hành. Không sửa `panel-reference/`. Runner cũ `scripts/test-debian12.sh` chỉ dành cho cây source trên VM trống; gói standalone này dùng `lab.sh`.
