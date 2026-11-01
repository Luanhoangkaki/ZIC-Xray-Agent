# Installer tương tác Debian 12

Installer chỉ có đúng menu dưới và ba trường tiếp theo. **Bản hiện tại cài được VLESS trong phạm vi adapter đã kiểm chứng; các mục 1/2/3 được nhận diện đúng API nhưng bị từ chối trước khi cài service.** Không tuyên bố agent đã hỗ trợ đủ bốn giao thức. Agent sau cài chạy `observe`, không bật proxy hay gửi traffic POST. Chưa production-ready.

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

API Key được nhập bằng `read -r` bình thường, hiển thị ký tự trên TTY theo yêu cầu ngày 2026-10-31; không dùng `read -s`. Chương trình không in lại giá trị này vào stdout/stderr, lỗi, log hoặc báo cáo. Người dùng nhập từng giá trị rồi Enter. Không có prompt xác nhận, lựa chọn core, menu nâng cấp hoặc hỏi lại; lỗi thì dừng với exit khác 0. Node ID phải là số thập phân dương chuẩn, tối đa `9223372036854775807`; không nhận dấu, khoảng trắng, số 0 hay leading zero. Domain tự chuẩn hóa sang HTTPS; không nhận HTTP, credentials/query/fragment trong URL. API Key ở đây là **server_token của V2B-ZIC**, không suy rằng API key người dùng/admin dùng được.

## Hợp đồng và cổng kiểm tra giao thức

Đối chiếu read-only `panel-reference/` commit `f778400907da4c47f0466abcab21030009e2770e`: UniProxyController và ServerService. Cơ chế tương tác lấy cảm hứng từ trình cài V2Node; không chạy/cài/nhúng V2Node và không chuyển sang endpoint hoặc node_type=v2node.

| Menu | node_type UniProxy | Panel trả gì | Quyết định bản này |
| --- | --- | --- | --- |
| 1 Shadowsocks | `shadowsocks` | cipher, obfs, server_port; SS2022 có server_key | Xray có protocol, nhưng agent chưa có adapter: từ chối |
| 2 Trojan | `trojan` | host, network, networkSettings, server_port, server_name | Xray có protocol, nhưng agent chưa có adapter/certificate policy: từ chối |
| 3 Hysteria | `hysteria` | bắt buộc đọc `version` | Version 1: không được core pin hỗ trợ; version 2: core có, nhưng agent chưa có adapter; cả hai chưa cài |
| 4 VLESS | `vless` | schema VLESS của commit đã pin | Chỉ cài observe sau khi tất cả tiền kiểm PASS |

Panel alias `hysteria2` về model `hysteria` trong constructor, nhưng config vẫn trả `version`, và payload bản 1/2 khác nhau. Installer luôn query `node_type=hysteria` cho mục 3 rồi phân biệt `version=1`/`version=2`, chấp nhận cả integer và chuỗi số chính xác `"1"`/`"2"` vì model không khai báo cast version. Thiếu/sai/null/trùng key đều dừng, không mặc định version 2. Source Xray v26.10.29 có `hysteria` server/client nhưng yêu cầu `version == 2`; không suy từ tên protocol rằng Hysteria1 được hỗ trợ. Nguồn: [Xray hysteria config](https://github.com/XTLS/Xray-core/blob/v26.10.29/infra/conf/hysteria.go), [protocol loader](https://github.com/XTLS/Xray-core/blob/v26.10.29/infra/conf/xray.go).

Probe protocol ngoài VLESS chỉ GET `/api/v1/server/UniProxy/config` với đúng token/node_id/node_type để xác nhận auth/node/version, rồi từ chối; không lấy users, không lưu credentials hoặc start. Daemon vẫn chỉ nhận VLESS; việc thêm menu không nới runtime protocol.

VLESS cần config và user API hợp lệ, ParseVLESS không hạ security/transport, không bỏ routes/encryption/PQ hoặc speed/device limits chưa thực thi. Chỉ TCP/raw, REALITY/Vision đã có adapter; TLS certificate, WS/gRPC/XHTTP và chính sách chưa hỗ trợ bị từ chối. Plaintext chỉ được preflight khi panel là HTTPS loopback. Không cấp quyền từ cache hoặc tự nới TLS khi lỗi.

## Một lệnh cài

Gói tự chứa binary Go, core chính thức riêng để **kiểm tra offline**, service sandbox và rollback. Bộ test executable tách riêng khỏi installer. Candidate mới tại `dist/release-candidates/v1.0.0-audit2-test/`, tag thử `v1.0.0-audit2-test`; bộ audit2 trước giữ riêng. Không cần Go/Python/Node/PHP/Docker hoặc tải dependency lúc cài. Core đóng gói không được cài thành dịch vụ hay thay core hiện tại.

File amd64:

```text
zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz
zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz.sha256
```

Arm64 dùng tên `zic-xray-agent-installer-debian12-arm64-v1.0.0.tar.gz` và `.sha256` trong cùng candidate mới. Chọn đúng kiến trúc khi chuyển gói. Không tự publish URL tải hoặc dùng curl|bash từ nguồn chưa xác minh.

Sau khi chuyển hai file bằng kênh tin cậy và giải nén:

```bash
sha256sum --check zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz.sha256
tar -xzf zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz
cd zic-xray-agent-installer-debian12-amd64-v1.0.0
```

Chạy đúng một lệnh để nhập và cài:

```bash
sudo bash ./install.sh
```

Cần Debian 12 + systemd, bash, coreutils, util-linux/flock, awk và phiên TTY để nhập bình thường. Không nhận credentials qua argv/env. Bản phát triển chỉ nên thử panel/UUID giả trên máy được phép; các test cục bộ chỉ dùng HTTPS loopback mock và không kết nối production.

Mặc định installer dùng `validation/linux-ARCH/xray` chính thức v26.10.29 để kiểm tra capability/config; không phải phép xác nhận core đang chạy. Nếu cần preflight đúng binary hiện hữu, cung cấp đường dẫn bằng cùng một lệnh, không thêm câu hỏi:

```bash
sudo env XRAY_BINARY=/usr/local/bin/xray bash ./install.sh
```

Binary phải trùng hash executable chính thức đã pin. Phiên bản/build khác bị từ chối thay vì coi là tương thích. Các hash Linux được lấy từ archive official đã đối chiếu digest metadata GitHub:

| Target | SHA256 executable v26.10.29 |
| --- | --- |
| linux/amd64 | `e783b9d2b46ed1ab3617c59b04dfbce38c999322d018c6e060f18d88bf6158aa` |
| linux/arm64 | `f02fa77fa2f1a0f8f2b4fd69af5000494bcc3fa4e97899caf023c7f72fd74474` |

Tiền kiểm sao chép core vào staging riêng, xác minh lại hash rồi chạy `run -test` với JSON qua stdin, confdir trống và môi trường đã lọc; không tạo file JSON tiền kiểm chứa UUID/private key. Không đọc config Xray hiện tại, không mở listener, không start/restart core/service gốc. stdout/stderr core bị bỏ để không lộ UUID/private key; lỗi được báo bằng thông điệp cố định. Deadline core 30 giây, tổng preflight 60 giây. Source lệnh [Xray run](https://github.com/XTLS/Xray-core/blob/v26.10.29/main/run.go) trả kết quả test trước `server.Start()`.

Sau preflight thành công mới publish:

- `/etc/zic-xray-agent/`: 0700, config.json và panel-config.json 0600.
- `/usr/local/lib/zic-xray-agent/zic-xray-agent`: binary riêng.
- `/etc/systemd/system/zic-xray-agent.service`: giữ nguyên DynamicUser, LoadCredential, sandbox và giới hạn hiện có; không có ExecStartPost đọc credentials.
- `/var/lib/zic-xray-agent-installer/install.XXXXXXXX`: receipt/evidence/rollback riêng 0700.

Installer gọi daemon-reload, enable/start **chỉ zic-xray-agent.service**, rồi đợi tối đa 45 lượt cách nhau 1 giây để xác nhận active và snapshot mới, đúng identity, chính sách được hỗ trợ. Mặc định đồng bộ 60 giây, honor interval panel đã validate. Service chỉ một agent process, warning/error, GOMAXPROCS=1, GOMEMLIMIT=12MiB, MemoryHigh=32M, MemoryMax=64M, CPUQuota=20%, TasksMax=64. Đây là giới hạn cấu hình, không phải số đo RAM/CPU.

Observe chỉ đồng bộ snapshot. Không cấu hình Stats endpoint qua menu này, không áp user vào Xray, không báo đã bật node và không POST traffic. Không sửa panel/DB/Redis/queue; không chạm `/opt/zic-xray-lab`, các zic-lab-* hay xray.service. Agent/config/unit/state/log cũ làm installer dừng trước khi hỏi, không ghi đè hay tự nâng cấp.

## Rollback

Khi cài lỗi, installer dùng receipt để stop/disable và di chuyển **chỉ file mới do lần cài này tạo** vào thư mục `removed` riêng của transaction. Không xóa credentials, không rewind state/log/ledger và không sửa Lab/Xray. Nếu rollback lỗi, báo chưa hoàn tất và giữ evidence; không hứa atomic qua mất điện/SIGKILL hoặc lỗi filesystem. Thư mục config/binary/unit có thể cần review thủ công nếu bị ngắt đúng giữa publish và ghi receipt.

Khi cài thành công, console in lệnh rollback chính xác, ví dụ (thay XXXXXXXX bằng giá trị đã in):

```bash
sudo bash /var/lib/zic-xray-agent-installer/install.XXXXXXXX/rollback.sh rollback /var/lib/zic-xray-agent-installer/install.XXXXXXXX
```

Rollback kiểm tra checksum file đã cài trước khi stop; nếu có chỉnh sửa mới hoặc drop-in mới thì từ chối để tránh đè thay đổi. Dữ liệu runtime giữ lại sau rollback nên lần cài tiếp theo sẽ từ chối state cũ; không xóa ledger để cài lại cho PASS.

## Bộ kiểm thử

Từ thư mục đã giải nén, không cần root cho test:

```bash
bash tests/run.sh
```

Runner checksum gói, tạo core và unit verifier tạm, chạy rootless UI/transaction mock rồi 11 test binary đúng kiến trúc. Mọi panel trong test là HTTPS loopback mock, UUID/token giả. Một số suite chạy Xray tạm với config/port riêng trong temp và cleanup sau test; không dùng core service đang chạy. Không cần cài agent để chạy test. Evidence giữ trong thư mục temp riêng in cuối lệnh.

Test bao gồm menu đúng/thứ tự/không lộ key qua xtrace/argv/output, input sai và int64 biên, mapping protocol, Hysteria1/2 và version thiếu/sai/trùng, unsupported không cài, auth/config/user policy fail, core sai hash, offline VLESS/REALITY, không bind port đã bị chiếm, không đọc confdir từ env, snapshot startup, autostart/rollback lỗi và không chạm Lab. Test mode/quyền TTY thực và systemd lifecycle phải chạy trên Debian; Git Bash chỉ mock chúng.

## Build lại

Trên Windows build host đã có Go và hai archive official Xray được xác minh:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1 -IncludeTests
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/package-installer.ps1
```

Packager tạo cả amd64/arm64, giữ release cũ, từ chối ghi đè artifact cùng tên. `dist/SHA256SUMS` có 6 tool binary + 22 test binary. Source test và evidence đi kèm từng gói. Các target Linux được cross-build trên Windows; chưa phải kết quả chạy Debian. Không có số đo VPS mới hoặc tuyên bố production-ready.
