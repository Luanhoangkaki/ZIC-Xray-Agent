# ZIC-Xray-Agent v1.0.0: cài trên Debian 12

Repository: https://github.com/Luanhoangkaki/ZIC-Xray-Agent

**TEST ONLY / OBSERVE MODE / VLESS subset / NOT PRODUCTION READY.** Tag dự kiến: `v1.0.0-audit2-test`, không phải tag chính thức `v1.0.0`. Tên asset giữ base version `v1.0.0`; chỉ dùng hash của candidate `dist/release-candidates/v1.0.0-audit2-test/`. Không dùng installer `20261031-r1` hoặc candidate cũ. Lệnh một dòng có pin SHA bootstrap nằm trong `ONE_LINE_AFTER_APPROVAL.txt` của candidate mới; cùng một lệnh tự chọn AMD64/ARM64. URL chưa được publish, chưa chạy kiểm tải GitHub thật.

**Bản chuẩn bị để review, chưa publish. Không production-ready.** Các URL `/releases/download/v1.0.0-audit2-test/` chỉ hoạt động sau khi chủ repository tự tạo tag và upload đúng các asset đã duyệt. Không có thao tác GitHub ghi nào được thực hiện trong lần chuẩn bị này.

Gói installer tự chứa agent Go, Xray chính thức v26.10.29 riêng để tiền kiểm offline, service sandbox và rollback. Không nhúng core vào agent; không cài hoặc restart Xray. Không chứa test binary, dữ liệu runtime, credentials, certificate/private key hoặc source panel. Bộ test và log được giữ riêng để review, tránh đưa fixture test/certificate của thư viện vào gói cài.

## Giao diện và hỗ trợ thực tế

```text
1. Shadowsocks
2. Trojan
3. Hysteria
4. VLESS
Nhập lựa chọn [1-4]:
Nhập tên miền Panel:
Nhập API Key Panel:
Nhập Node ID:
```

Nhập số và Enter, sau đó nhập ba trường theo thứ tự trên. API Key hiển thị khi nhập, chính là server_token UniProxy của panel; không đưa vào argv, environment hoặc log. Không thêm menu/câu hỏi, không hỏi lại khi input sai. Node ID là số thập phân dương chuẩn trong int64.

Menu ánh xạ `shadowsocks`, `trojan`, `hysteria`, `vless` theo UniProxy V1 của V2B-ZIC commit `f778400907da4c47f0466abcab21030009e2770e`. Hysteria phải đọc version 1/2 từ config; alias model hysteria2 không chứng minh hai giao thức tương đương.

**Chỉ VLESS có adapter được kiểm chứng trong bản này**, với tập con TCP/raw và REALITY/Vision đã triển khai. Các mục Shadowsocks/Trojan/Hysteria1/Hysteria2 được nhận diện nhưng dừng trước lưu credential/cài service vì adapter agent chưa hoàn thiện. Không tự bỏ giới hạn người dùng, TLS, route hoặc transport chưa hỗ trợ để cài cho thành công.

Agent sau cài chỉ **observe**: đồng bộ snapshot, không mở proxy, không cấp user vào Xray, không POST traffic và không bật production management. Không có số đo mới để bảo đảm RSS <20 MB hoặc idle CPU <1%.

## Cài từ gói đã kiểm tra

Chỉ thử trên Debian 12 amd64/arm64 riêng được phép, không dùng production hoặc Lab đang hoạt động. Cần bash, systemd, coreutils, awk và util-linux/flock; có TTY để nhập bình thường. Không cần Go hoặc tải dependency lúc cài.

Ví dụ amd64, sau khi chuyển archive và `.sha256` qua kênh tin cậy:

```bash
sha256sum --check zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz.sha256
tar -xzf zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz
cd zic-xray-agent-installer-debian12-amd64-v1.0.0
sudo bash ./install.sh
```

Arm64 dùng `zic-xray-agent-installer-debian12-arm64-v1.0.0.tar.gz` và `.sha256` tương ứng. Trong thư mục gói, lệnh cài tương tác duy nhất là `sudo bash ./install.sh`.

Installer từ chối Agent/config/state/unit cũ thay vì ghi đè. Sau tiền kiểm auth/node/config/users/policy và core thành công, mới publish config 0700/file 0600 và enable/start **chỉ zic-xray-agent.service**. Nó đợi active cùng snapshot mới đúng identity; lỗi thì rollback phần vừa cài. DynamicUser, LoadCredential, sandbox và giới hạn tài nguyên giữ nguyên. Không chạm zic-lab-*, /opt/zic-xray-lab, xray.service, panel, DB, Redis hay queue.

## Tải đúng phiên bản bằng script cố định

Không dùng `latest`, curl|bash hoặc chạy archive trước khi kiểm checksum. Script `get-zic-xray-agent-v1.0.0.sh` được sinh cùng hai SHA256 ghim sẵn. Nó bắt buộc sidecar trùng pin, kiểm bytes archive trước mọi lệnh tar, từ chối path/link nguy hiểm và kiểm manifest bên trong sau giải nén.

Sau khi Release đã được chủ repository publish, tải script và checksum rồi kiểm trước khi chạy:

```bash
curl --fail --location --proto '=https' --proto-redir '=https' --output get-zic-xray-agent-v1.0.0.sh https://github.com/Luanhoangkaki/ZIC-Xray-Agent/releases/download/v1.0.0-audit2-test/get-zic-xray-agent-v1.0.0.sh
curl --fail --location --proto '=https' --proto-redir '=https' --output get-zic-xray-agent-v1.0.0.sh.sha256 https://github.com/Luanhoangkaki/ZIC-Xray-Agent/releases/download/v1.0.0-audit2-test/get-zic-xray-agent-v1.0.0.sh.sha256
sha256sum --check get-zic-xray-agent-v1.0.0.sh.sha256
bash ./get-zic-xray-agent-v1.0.0.sh
```

Mặc định chỉ tải, kiểm và giải nén vào temp riêng; chưa chạy installer. Đối chiếu thêm hash script với báo cáo đã được duyệt qua kênh tin cậy: checksum tải cùng nguồn là kiểm toàn vẹn, không thay thế chữ ký hoặc việc tin cậy nguồn script.

Chỉ khi người vận hành chủ động cài trên máy thử nghiệm trống:

```bash
bash ./get-zic-xray-agent-v1.0.0.sh --install
```

`--install` từ chối khi phát hiện service Lab/Agent/Xray quen thuộc đang active; không bảo đảm nhận diện mọi production service đặt tên riêng. Nó không thêm câu hỏi vào UI installer. Phiên chuẩn bị Release này không chạy chế độ đó.

Core mặc định trong gói chỉ chạy `run -test` bằng private verified copy và JSON qua stdin, không ghi file JSON tiền kiểm chứa UUID/private key, không kiểm chứng core đang chạy. Có thể truyền `XRAY_BINARY=/absolute/path/to/official/xray` khi tự chạy install.sh để tiền kiểm binary hiện hữu; binary khác hash v26.10.29 đã pin bị từ chối.

Xem `UPDATE_ROLLBACK.md`, `TEST_RESULTS.md` và `SECURITY_SCAN.json` trước khi quyết định cài. Linux/systemd chưa chạy thì vẫn là NOT RUN, không nâng thành PASS do cross-build.
