# Sửa startup credentials/PID trên Debian 12

Gói vá: `zic-xray-lab-startup-fix-20261031-r1.tar.gz` và file `.sha256` cùng tên. Chỉ sửa startup của lab cách ly đã cài, không thay binary Go/Xray, agent unit, JSON cấu hình, UUID/token, ledger hoặc dữ liệu panel. Agent tiếp tục observe, không POST. Không triển khai từ máy build, không tự reboot. Chưa có kết quả thực thi systemd/Debian VPS cho bản vá này.

## Nguyên nhân và diff đã xem trước khi sửa

Trong source systemd **v252**, `service_enter_start()` gọi tiến trình chính với `EXEC_WRITE_CREDENTIALS`, còn `service_enter_start_post()` không dùng cờ này. Trong `apply_mount_namespace()` của `execute.c`, `creds_path` chỉ được truyền khi có cờ trên. Vì vậy dưới sandbox filesystem, cùng CREDENTIALS_DIRECTORY và UID không bảo đảm main và ExecStartPost nhìn cùng credential mount. Cách cũ đặt `zic-lab ready --config ${CREDENTIALS_DIRECTORY}/lab.json` trong post-start không tương thích môi trường đã báo; chạy readiness độc lập thành công không kiểm tra được namespace của post-start.

Nguồn chính thức: [service.c v252](https://github.com/systemd/systemd/blob/v252/src/core/service.c), [execute.c v252](https://github.com/systemd/systemd/blob/v252/src/core/execute.c). Chưa đọc journal/version chính xác trên VPS từ máy này, nên không khẳng định mọi bản vá Debian của systemd 252 đều có cùng lỗi; source và triệu chứng đã báo khớp cơ chế trên.

PID trước đây cũng nằm trong ExecStartPost. Khi bỏ toàn bộ post-start để khởi động core, bước tạo `/run/zic-lab-core/xray.pid` bị bỏ theo. `RuntimeDirectory=` chỉ tạo thư mục; systemd không tự ghi file PID cho chương trình. `/run` mất sau reboot, PID cũ không dùng được sau restart.

Diff unit chỉ thay `Type=simple` thành `Type=notify`, thêm `NotifyAccess=all`, `TimeoutStartSec=60s`, và thay các ExecStart/ExecStartPost bằng `/bin/sh /opt/zic-xray-lab/start.sh panel|core`. Các dòng DynamicUser, LoadCredential, filesystem/network sandbox, MemoryMax, TasksMax, Restart/RestartSec giữ nguyên. Agent unit không đổi byte nào.

Launcher tạo readiness child ngay trong ExecStart, kế thừa namespace/UID/credentials. Sau khi GET mock hoặc API Xray kiểm tra thành công, child dùng `/usr/bin/systemd-notify --ready --pid=...` với barrier mặc định. NotifyAccess=all cần thiết cho child trong cùng cgroup; không cấp thêm capability hoặc chạy root. Không dùng `--no-block` vì có thể mất thông báo khi child thoát; xem [systemd-notify 252](https://www.freedesktop.org/software/systemd/man/252/systemd-notify.html).

Core launcher gọi `write-pid` với PID của chính nó, rồi `exec` binary Xray chính thức. `exec` giữ PID; steady-state main vẫn là Xray, không thêm supervisor. Helper chỉ tồn tại lúc startup, có deadline readiness 45 giây và giới hạn start của unit 60 giây. Nếu đọc credential, ghi PID hoặc readiness thất bại, service không được xác nhận ready. Mỗi restart và boot chạy lại launcher, tự cập nhật file PID 0644 trong runtime dir 0755; credential nguồn vẫn 0600/config dir 0700.

Installer mới cài thêm start.sh và kiểm tra Type=notify, không còn post-start, MainPID trùng file PID, `/proc/PID/exe` là core lab, cùng readiness. Nếu lần kích hoạt mới thất bại, chỉ ba unit mới của lab bị disable/stop, giữ file và evidence.

## Triển khai vào lab đang hoạt động

Dành cho **Debian 12 amd64 lab riêng**, ba service lab hiện đang active với workaround. Cần systemd-notify (đi kèm systemd Debian), bash, coreutils, diffutils, util-linux/flock. Chuyển cả archive và `.sha256` vào cùng thư mục bằng kênh tin cậy. Không dùng installer `install` để ghi đè lab cũ.

```bash
sha256sum --check zic-xray-lab-startup-fix-20261031-r1.tar.gz.sha256
tar -xzf zic-xray-lab-startup-fix-20261031-r1.tar.gz
cd zic-xray-lab-startup-fix-20261031-r1
sha256sum --check --strict SHA256SUMS
sudo env ZIC_LAB=1 bash update-startup.sh check
```

`check` xác minh hash, OS, service origin, không có drop-in chưa review, readiness GET loopback và systemd-analyze verify, rồi in diff hai unit đang cài so với bản sửa. Chưa thay file startup hay restart service. `CHANGES.patch` trong archive là diff source đã chuẩn bị trên máy build.

Nếu báo drop-in: dừng ở đây, sao lưu và xem `sudo systemctl cat zic-lab-panel.service zic-lab-core.service zic-lab-agent.service`. Chỉ xử lý drop-in workaround của lab sau review; không chạy `systemctl revert`, không xóa drop-in chung hoặc sửa quyền credentials. Script chủ động từ chối drop-in thay vì tự xóa cấu hình không hiểu. Sau khi xử lý, chạy daemon-reload và check lại. Nếu workaround nằm trực tiếp trong unit, updater lưu đúng bản đang chạy đó để rollback.

Sau khi xem diff, áp dụng:

```bash
sudo env ZIC_LAB=1 bash update-startup.sh apply
sudo bash /opt/zic-xray-lab/lab.sh startup-check
```

Apply giữ lock, sao lưu sáu file đích (hoặc ghi nhận chưa tồn tại) vào `/opt/zic-xray-lab/results/startup-fix.backup.XXXXXXXX` mode 0700, kèm hash, metadata trước/sau và rollback.sh. Chỉ stop/start ba zic-lab-* để thay launcher, lab.sh, và hai unit panel/core ở bản live lẫn bản mirror. Enable/disable flags hiện có không đổi. Không sửa config, agent unit/binary, core binary, JSON Xray, state/log, không gửi POST, không mở endpoint ngoài loopback và không gọi xray.service. Thay file bằng rename; nếu apply/start/kiểm tra lỗi, tự khôi phục các file cũ và thử khởi động workaround cũ. Apply vẫn exit khác 0 khi đã phải rollback.

Đọc backup path từ output; không dùng wildcard để chọn backup. Nếu mất điện/SIGKILL giữa thao tác nhiều file, không có bảo đảm transaction toàn filesystem: giữ backup/evidence và chạy rollback có kiểm tra. Nếu rollback cũng lỗi, chỉ lab bị dừng để chờ review; không xóa/rebase ledger hoặc tự nới sandbox.

## Kiểm thử Debian sau vá

Không chạy đồng thời với workload khác:

```bash
sudo bash /opt/zic-xray-lab/lab.sh startup-check
sudo bash /opt/zic-xray-lab/lab.sh restart-check
sudo bash /opt/zic-xray-lab/lab.sh core-restart-check
sudo bash /opt/zic-xray-lab/lab.sh panel-restart-check
sudo bash /opt/zic-xray-lab/lab.sh test --quick
```

`core-restart-check` restart core + agent cùng transaction; `panel-restart-check` restart đủ ba unit do quan hệ Requires/After có sẵn. Sau mỗi lần, kiểm tra notify, PID/executable, readiness và ledger recovery. Agent restart không đổi core epoch; core/panel restart phải đổi core epoch và thêm incident cho tail không quan sát được. Giữ incident, không tự acknowledge hoặc hứa tuyệt đối không mất byte.

Kiểm tra crash tự restart của core (chỉ lab) khi không có tải: ghi `MainPID` trước, dùng `sudo systemctl kill --kill-whom=main --signal=SIGKILL zic-lab-core.service`, đợi RestartSec=5s và startup, rồi kiểm tra `systemctl show -p MainPID -p NRestarts -p Type zic-lab-core.service` cùng `sudo cat /run/zic-lab-core/xray.pid`. Hai PID phải trùng và khác PID cũ; xem journal nếu service bị start-limit. Quan hệ Requires có thể dừng agent khi core bị kill; chạy `sudo systemctl start zic-lab-agent.service` rồi startup-check. Không thêm thay đổi dependency ngoài phạm vi bản vá này.

Reboot toàn VPS chỉ khi người vận hành cho phép trên lab riêng:

```bash
sudo bash /opt/zic-xray-lab/lab.sh prepare-reboot
sudo reboot
# Sau khi đăng nhập lại:
sudo bash /opt/zic-xray-lab/lab.sh startup-check
sudo bash /opt/zic-xray-lab/lab.sh verify-reboot
sudo bash /opt/zic-xray-lab/lab.sh test --quick
```

Ba service phải đã enable từ lần cài lab; updater giữ nguyên trạng thái enable. Sau boot phải active, file PID tự xuất hiện và trùng MainPID, boot/core epoch đổi, byte đã biết giữ nguyên. `startup-check` xác minh file PID bằng MainPID và executable; systemctl báo active cho Type=notify nghĩa là systemd đã nhận readiness. Xem evidence recovery tại results và `sudo journalctl -u zic-lab-panel -u zic-lab-core -u zic-lab-agent --since '-10 min' --no-pager`; không chia sẻ nội dung credential.

## Rollback

Updater in chính xác lệnh rollback với backup path. Ví dụ, **thay XXXXXXXX bằng tên đã in**, không đoán/chọn tự động:

```bash
sudo env ZIC_LAB=1 bash /opt/zic-xray-lab/results/startup-fix.backup.XXXXXXXX/rollback.sh rollback /opt/zic-xray-lab/results/startup-fix.backup.XXXXXXXX
```

Rollback kiểm tra hash backup và các file đã apply; từ chối trước khi stop nếu có sửa đổi mới hoặc drop-in chưa review. Khôi phục đúng startup/unit workaround cũ, giữ nguyên dữ liệu đang có. Không restore PID cũ: khi core cũ khởi động lại, kiểm tra executable rồi ghi PID mới trước khi start agent. Nếu unit cũ vẫn lỗi, rollback báo lỗi và dừng lab; không coi rollback là PASS. Không rollback traffic.json/counter/queue để che gap do core restart.

## Kiểm chứng cục bộ và giới hạn

`bash scripts/test-lab-startup.sh` dùng command/filesystem mock trong temp: credential kế thừa, notify sau ready, PID được giữ qua exec và đổi qua các lần start, giả lập mất runtime PID, readiness/PID lỗi không notify/chạy core; apply + rollback thủ công; activation lỗi tự rollback; từ chối rollback đè sửa đổi mới; từ chối drop-in trước thay đổi. Trên Git Bash, ownership/mode và systemd được mock rõ ràng, không phải bằng chứng Linux. Test không gọi systemctl thật, không mở socket, không chạm /etc hay /opt thật.

Bản vá chưa được chạy systemd/restart/reboot trên Debian VPS từ môi trường Windows này. Cần thực thi các bước acceptance trên máy lab; không production-ready, không có kết quả RAM/CPU mới và không bật traffic thật.
