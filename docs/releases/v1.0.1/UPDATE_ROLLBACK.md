# v1.0.1: cập nhật và rollback

Chỉ là hướng dẫn để chủ máy review và tự thực hiện trên máy thử nghiệm riêng. Không có lệnh bên dưới được chạy trên VPS/production/Lab đang hoạt động trong lần chuẩn bị Release. Không có tự cập nhật trong daemon.

## Cập nhật từ bản thử nghiệm trước

Installer v1.0.1 chỉ dành cho **cài mới**, cố ý từ chối config/unit/binary/state cũ. Không chạy installer đè lên bản đang cài và không xóa state để vượt kiểm tra. Chưa có migration/upgrade tự động được kiểm chứng; quy trình thủ công dưới giữ config/ledger hiện tại, chỉ thay binary/unit sau khi đã được phép dừng agent trên máy test.

1. Tải/kiểm SHA256/giải nén v1.0.1 bằng chế độ extract-only. Chạy các kiểm thử Linux trên máy mới theo TEST_RESULTS.md trước khi cập nhật máy đang thử nghiệm. Xác nhận config cũ là observe, không có traffic sender hoặc schema chưa tương thích; nếu không chắc tương thích, dừng để review, không đổi mode hay reset ledger.
2. Ghi lại trạng thái enable, PID, unit/drop-in và checksum config hiện tại. Không áp unit mới nếu có custom drop-in chưa review. Tạo backup root riêng, không đưa backup vào gói Release hoặc repository:

```bash
sudo install -d -m 0700 /var/lib/zic-xray-agent-upgrades
backup=$(sudo mktemp -d /var/lib/zic-xray-agent-upgrades/v1.0.1.XXXXXXXX)
printf 'Upgrade backup: %s\n' "$backup"
sudo cp -p /usr/local/lib/zic-xray-agent/zic-xray-agent "$backup/agent"
sudo cp -p /etc/systemd/system/zic-xray-agent.service "$backup/zic-xray-agent.service"
sudo sha256sum /etc/zic-xray-agent/config.json /etc/zic-xray-agent/panel-config.json | sudo tee "$backup/config-before.sha256" >/dev/null
sudo systemctl show zic-xray-agent.service --property Id,MainPID,ActiveState,UnitFileState,DropInPaths | sudo tee "$backup/service-before.txt" >/dev/null
```

3. Trên root/test session đã kiểm tra, đặt BUNDLE đúng thư mục vừa xác minh, chọn kiến trúc phù hợp, rồi dừng duy nhất agent. Nếu một bước lỗi, không tiếp tục start; dùng phần rollback dưới. Không có yêu cầu restart Xray:

```bash
BUNDLE=/absolute/path/to/zic-xray-agent-installer-debian12-amd64-v1.0.1
arch=$(dpkg --print-architecture)
test "$arch" = amd64 || test "$arch" = arm64
(cd "$BUNDLE" && sha256sum --check --strict SHA256SUMS)
sudo systemctl stop zic-xray-agent.service
sudo install -m 0755 "$BUNDLE/bin/linux-$arch/zic-xray-agent" /usr/local/lib/zic-xray-agent/agent.v1.0.1.new
sudo mv -T /usr/local/lib/zic-xray-agent/agent.v1.0.1.new /usr/local/lib/zic-xray-agent/zic-xray-agent
sudo install -m 0644 "$BUNDLE/packaging/zic-xray-agent.service" /etc/systemd/system/zic-xray-agent.service.v1.0.1.new
sudo mv -T /etc/systemd/system/zic-xray-agent.service.v1.0.1.new /etc/systemd/system/zic-xray-agent.service
sudo systemd-analyze verify /etc/systemd/system/zic-xray-agent.service
sudo sha256sum --check --strict "$backup/config-before.sha256"
sudo systemctl daemon-reload
sudo systemctl start zic-xray-agent.service
```

Không thay enable flag, credentials, panel-config.json, /var/lib/zic-xray-agent hoặc log/queue. Unit v1 giữ profile sandbox/tài nguyên của bản đang chuẩn bị; không nhận cấu hình Xray hiện hữu làm template.

4. Kiểm tra `systemctl is-active`, PID/NRestarts, log warning/error và snapshot sau sync. Có thể dùng lệnh đọc cục bộ sau, chờ sync mới nếu cần (mặc định 60 giây, tối đa 45 lượt cách 1 giây khi kiểm startup):

```bash
sudo systemctl is-active zic-xray-agent.service
sudo /usr/local/lib/zic-xray-agent/zic-xray-agent install-status --config /etc/zic-xray-agent/config.json --state-dir /var/lib/zic-xray-agent/data
```

install-status chỉ chứng minh snapshot mới/identity/policy được kiểm tra; không chứng minh bật proxy, billing, không mất traffic hoặc production-ready. Cần kiểm restart/reboot thực trên Debian 12 riêng, không dùng số đo Windows làm bằng chứng VPS.

## Rollback sau cập nhật thủ công

Thay BACKUP bằng đúng đường dẫn đã ghi lại, không tự chọn bằng wildcard. Giữ config/state/ledger mới nhất; không chép checkpoint cũ đè lại vì có thể làm cộng lặp hoặc mất batch:

```bash
BACKUP=/var/lib/zic-xray-agent-upgrades/v1.0.1.XXXXXXXX
sudo systemctl stop zic-xray-agent.service
sudo install -m 0755 "$BACKUP/agent" /usr/local/lib/zic-xray-agent/agent.rollback.new
sudo mv -T /usr/local/lib/zic-xray-agent/agent.rollback.new /usr/local/lib/zic-xray-agent/zic-xray-agent
sudo install -m 0644 "$BACKUP/zic-xray-agent.service" /etc/systemd/system/zic-xray-agent.service.rollback.new
sudo mv -T /etc/systemd/system/zic-xray-agent.service.rollback.new /etc/systemd/system/zic-xray-agent.service
sudo systemctl daemon-reload
sudo systemctl start zic-xray-agent.service
sudo sha256sum --check --strict "$BACKUP/config-before.sha256"
```

Nếu bản cũ không đọc được state hiện tại hoặc không khởi động, dừng agent và giữ evidence để đối soát; không tự hạ schema/reset traffic.json hay khẳng định rollback thành công. Quy trình này chưa được chạy trên Debian trong lần chuẩn bị, được ghi NOT RUN.

## Rollback cài mới bằng installer

Installer in lệnh rollback riêng cho transaction mới, ví dụ:

```bash
sudo bash /var/lib/zic-xray-agent-installer/install.XXXXXXXX/rollback.sh rollback /var/lib/zic-xray-agent-installer/install.XXXXXXXX
```

Nó kiểm checksum và drop-in trước khi stop/disable agent, chỉ di chuyển file nó đã tạo vào transaction/removed riêng; từ chối nếu có chỉnh sửa mới. Credentials được giữ private, state/log/ledger không bị xóa hoặc quay lui. Nếu lỗi giữa publish và receipt, mất điện/SIGKILL hoặc filesystem lỗi, có thể cần review thủ công; không có bảo đảm transaction toàn filesystem. Không chạm Xray, Lab hay panel.
