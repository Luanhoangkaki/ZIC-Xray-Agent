# Biên bản kiểm tra cục bộ

Ngày 2026-10-30 (Asia/Saigon). Host Windows/amd64; Go agent compiler `go1.26.5 windows/amd64`. Không có WSL/Linux được cài sẵn. Không truy cập panel production hoặc VPS, không bật service và không sửa cấu hình Xray hiện hữu.

## Source panel

Clone thành công repository riêng `panel-reference/`. Production commit resolve đúng, default master và dev được so sánh bằng Git; các file API được yêu cầu không khác. Reference không có working-tree changes. Xem hash/bằng chứng ở [panel-api.md](panel-api.md).

## Các kiểm tra đã thực hiện

- `go test -count=1 ./...`: các package unit test PASS; có chạy bài core thật khi đặt XRAY_BINARY. Trường hợp không đặt biến này thì bài core thật SKIP theo thiết kế.
- `go vet ./...`: PASS.
- Build `CGO_ENABLED=0`, `-trimpath -ldflags '-s -w'` cho Linux amd64/arm64: PASS. `dist/SHA256SUMS` được tạo lại từ artifact.
- `bash -n install.sh`, `bash -n scripts/build.sh`, installer `--help`: PASS trên Git Bash. Chưa thực thi cài đặt Linux.
- Unit test HTTP/TLS: schema đúng V2B-ZIC (không yêu cầu protocol), HTTP 500 auth/node source-backed, token có ký tự đặc biệt, lỗi không phản chiếu token/body, ETag config/user, 304 không có baseline bị từ chối, redirect không được theo, giới hạn body, JSON trùng key/quá sâu, thiếu/duplicate user, cấu hình unsupported không bị downgrade.
- Unit test state: checksum bị hỏng bị từ chối, kernel lock chống chạy trùng và nhả sau đóng, cache không advance ETag khi user request thất bại, cache được đọc lại sau khởi tạo runner mới, Stats checkpoint được lưu khi panel không phản hồi, epoch đổi giữa hai lần đọc bị từ chối.
- Unit test ledger: baseline, delta, replay cùng snapshot, mở lại ledger mô phỏng restart agent, counter giảm/biến mất, core epoch đổi, lỗi ghi đĩa và khác identity không âm thầm xóa/ghi đè dữ liệu.
- Unit test logging: rotation/retention hữu hạn và từ chối thông tin tùy ý thay vì đưa vào log.

## Bổ sung ở vòng hoàn thiện hiện tại

- Toàn bộ `go test -count=1 ./...`, `go vet ./...`, build agent/bench Linux amd64/arm64 và build test binary hai kiến trúc bằng `-IncludeTests`: PASS trên host Windows. Test Linux-specific chỉ được compile, chưa chạy.
- `GOOS=linux GOARCH=amd64 go vet ./...` và bản arm64 cũng PASS; đây là phân tích tĩnh, không phải chạy Linux.
- Outbox atomic: pending chuyển sang batch trong cùng checkpoint, gộp batch chưa gửi, payload sau SENDING giữ nguyên, đọc lại counter/restart không cộng lặp; lỗi ghi queue không xóa pending trong RAM/disk.
- Crash tiến trình thật (subprocess `os.Exit`, không defer): daemon chết sau checkpoint trước sync, restart hai lần giữ đúng delta; OS lock tự nhả. Sender chết trước POST hoặc sau mock đã nhận POST nhưng trước checkpoint response: disk còn SENDING, recovery ghi UNKNOWN, không có request lặp.
- HTTP mock: data=true thành ACCEPTED; timeout sau nhận dữ liệu, 500, JSON sai/duplicate/field lạ thành UNKNOWN; redirect POST không được theo dù caller cấu hình lỏng; đích ngoài loopback và default sender đều bị chặn trước attempt.
- CLI status không sửa SENDING/không lộ user payload; resolve cần digest/decision/evidence, not-applied không requeue; CLI send không tồn tại.
- Core controller: intent/ownership/drift; crash ở Add/Remove/users được tiếp tục bằng inspect, phục hồi inbound thiếu; policy speed/device unsupported tắt inbound thuộc sở hữu. Checkpoint Stats lỗi vẫn thử thu hồi và trả lỗi gap.
- Lịch API backoff không làm chậm Stats/queue (bài chạy 6,5 giây); cache quyền quá hạn hoặc future clock không được tin.
- Official core fixture đã thêm lifecycle controller thật và luồng daemon → HTTPS mock source-shaped → Xray thật: ETag, thay cổng, quay UUID, loại user cũ, xác nhận state và shutdown xóa inbound thử. Không gửi POST từ daemon.
- Fingerprint có test hồi quy 200 lần dựng cùng config: protobuf mã hóa xác định, tránh config không đổi bị recreate do thứ tự field.
- Lỗi ledger/control nghiêm trọng trong manage-test sẽ thử fence inbound đã chứng minh sở hữu, không gọi lại checkpoint từ RAM có thể cũ sau rename/fsync lỗi. Fence không chạm inbound lạ, không dừng core/service toàn cục. TLS policy chưa triển khai không bị bỏ qua.
- Script test Debian 12 kiểm tra opt-in/OS/checksum và chạy các test binary đã dựng; Bash syntax PASS. Script chưa được thực thi trên Linux hoặc VM thật; systemd verify/lifecycle chưa PASS.
- Artifact cuối có 26 checksum đã đối chiếu: 4 binary agent/bench và 11 test suite cho mỗi kiến trúc. Hash bốn file panel-reference vẫn đúng audit ban đầu; không sửa panel source hoặc gọi panel thật.

## Bài tích hợp core thật

Tải duy nhất archive [official Xray v26.10.29 Windows 64](https://github.com/XTLS/Xray-core/releases/tag/v26.10.29) để chạy cục bộ. SHA256 archive đối chiếu với asset metadata chính thức: `fce33e81cccf50e7aab572b175519a50b5e0f9d2f5c8d1ed73012ea5c9ad4d58`. Executable báo `Xray 26.10.29 ... 0afec66 (go1.27.3 windows/amd64)`. Binary chỉ nằm trong `.tools/`, không được nhúng vào agent hoặc gói phát hành agent.

Fixture tạo một process tạm, tất cả listener trên 127.0.0.1, cấu hình trong thư mục test; cleanup kill/wait process. Từng thao tác sau đã chạy thật qua gRPC: AddInbound VLESS TCP, AddUser, GetInboundUsers, reconcile idempotent và thêm user thứ hai, QueryStats reset=false, thu hồi user có checkpoint trước, xác nhận user bị xóa, RemoveInbound, AddInbound REALITY và AddUser Vision.

Truyền dữ liệu qua hai UUID riêng ở các batch 10/20/30 kết nối TCP đồng thời (60 lượt tổng cộng), mỗi lượt 4096 byte upload và echo 4096 byte download. Tổng mỗi chiều 245,760 byte; mỗi user 122,880 byte mỗi chiều. So sánh counter và pending theo từng user, mở lại ledger và đọc counter lần nữa không làm tăng pending. Đây là kiểm tra dữ liệu của fixture, không phải benchmark VPS hoặc thử soak.

Lần chạy đầu của fixture thất bại vì core mới mặc định blackhole đích private của inbound VLESS. Đã đối chiếu source Freedom, thêm allow rule đúng loopback trong **fixture tạm**, rồi chạy lại thành công; không giảm chính sách trong agent hoặc cấu hình hiện hữu. Không che bài fail bằng nới assertion/counter download.

## Chưa được kiểm chứng

- Chạy request tới PHP V2B-ZIC thật, queue/Redis/job thực tế, mất response/queue retry và đối soát billing.
- Chạy binary agent daemon Linux, systemd-analyze verify, LoadCredential/DynamicUser, installer root/quyền file, stop/restart/SIGKILL/boot trên Debian/Ubuntu, cả amd64/arm64.
- Crash tiến trình Runner với Stats giả đã thử; restart daemon/core Linux thật giữa luồng tải, VM reboot, power-loss/fsync/disk corruption thật và backlog dài chưa thử. Epoch reboot mới chỉ mô phỏng và compile test /proc Linux.
- REALITY handshake đầu cuối; transport khác; thực thi speed/device limit, quota/expiry khi offline và cắt session đã xác thực.
- RSS/CPU của agent, core, toàn hệ thống trên VPS 1 vCPU/1 GB; idle 30 phút, tải 15 phút mỗi mức, soak 24 giờ. Không có số đo để kết luận <20 MB hoặc <1% CPU.
- Sender duy nhất là thư viện test loopback, không có gửi traffic thật hay durable panel receipt. Production manage vẫn chặn; manage-test chỉ lab. Không ghi dự án hoàn thành/sẵn sàng production dựa trên các bài test hiện có.

## Gói lab Debian 12 ngày 2026-10-31

Đây là kết quả mới nhất; các mục ngày 2026-10-30 phía trên là lịch sử. Chỉ kiểm tra trên Windows/amd64, không deploy/reboot VPS.

- `go test -count=1 -v ./...` với official Windows Xray v26.10.29: **PASS cả 11 package có test**, không SKIP. Log tại `dist/verification/windows-tests.log`. Bao gồm fixture lab được đóng gói, chặn POST mock, ledger từng UUID và từ chối evidence thiếu/sai/restart giữa pha.
- `go vet ./...` cho windows/amd64, linux/amd64 và linux/arm64: **PASS**, log `dist/verification/vet.log`.
- Build CGO_ENABLED=0 Linux amd64/arm64 cho agent, bench và lab: **PASS**. Build lại **22 test binary (11/kiến trúc)**. `dist/SHA256SUMS` hiện có **28** artifact, thay con số 26 ở vòng trước. Audit lại tất cả checksum và ELF machine 62/183: **PASS**, `dist/verification/artifact-audit.json`.
- Bash syntax cho installer, build, runner Debian và lab installer: **PASS**. `lab.sh --help` PASS. Chưa chạy install/systemd trên Linux.
- Panel-reference giữ đúng commit production, working tree sạch và bốn file contract không đổi: `dist/verification/panel-reference-audit.json`. Sửa một lỗi chép thiếu ký tự trong hash tài liệu của ServerRoute.php sau khi xác nhận working tree sạch; không sửa PHP/reference.
- Archive official Linux Xray v26.10.29 có SHA256 `8a5f3e6a1057e4bd0f8a7c57c9d91204b4be0b74222a6e210d070603101163c2`, khớp digest metadata GitHub lưu tại `dist/verification/xray-release.json`. Core đóng gói được extract trực tiếp từ archive đã xác minh. Windows archive/hash và executable cũng được đối chiếu lại.
- Gói `dist/zic-xray-agent-lab-debian12-amd64-20261031.tar.gz`: **53,435,069 byte**, SHA256 `c6dd1f2c4ebb403064defaf423c5e2c91eec6e537b6d51beafd404701aa5c91f`. Giải nén lại: 34 file trong manifest đều khớp checksum, có đúng 11 test suite amd64, script/unit LF và trợ giúp Bash PASS. Biên bản `dist/verification/bundle-check.json`.

Gói chỉ chứa observe, mock HTTPS/UUID giả, core riêng loopback và các unit zic-lab-*. Không có production send; mock đứng thường trực từ chối POST; test sender chỉ dùng mock loopback riêng. Agent một process, công cụ lab/bench là tiến trình kiểm thử độc lập. Có warm-up/idle, 10/20/30 kết nối, CPU/RSS riêng, thống kê mean/p95/peak, per-UUID Stats/ledger và lệnh recovery agent/core/reboot. Không tự reboot hoặc sửa dịch vụ Xray hiện hữu.

**Linux execution/VPS measurements: NOT RUN.** Chưa có số đo tài nguyên, độ ổn định dài hạn, systemd lifecycle hoặc reboot thật. Benchmark loopback dùng client/server chung core; không đại diện cho server REALITY ngoài mạng. Recovery core ghi incident cho tail chưa biết, không chứng minh tuyệt đối không mất byte. Không production-ready. Lệnh chính xác trong [debian12-lab.md](debian12-lab.md).

## Installer tương tác 2026-10-31

Đã thêm installer Debian 12 có đúng menu 1–4 và ba trường Panel domain/API Key nhập ẩn/Node ID. Chỉ VLESS trong phạm vi adapter đã kiểm chứng được cài ở observe; Shadowsocks/Trojan/Hysteria1/Hysteria2 được nhận diện đúng API/version nhưng từ chối trước khi cài vì chưa có adapter agent phù hợp. Không dùng V2Node daemon/core, không POST traffic và không thay Lab.

- Go `go test -count=1 -v ./...`: PASS 11 package có test, không SKIP, với official Windows Xray v26.10.29. Thêm mapping/version, auth/policy/core failures, VLESS/REALITY offline và snapshot startup. Lệnh -test vẫn PASS khi cổng VLESS đang bị test giữ, chứng minh phép tiền kiểm không bind server. Log: `dist/verification/interactive-go-tests.log`.
- Rootless UI/transaction mock trên Git Bash: PASS menu/thứ tự nhập, hidden-read, Node ID int64, không lộ API Key trong xtrace/argv/stdout/stderr hoặc environment kế thừa, autostart, rollback auth/unit/start/snapshot failures, giữ file cũ và từ chối đè chỉnh sửa mới. Test không gọi systemctl thật hoặc /dev/tty thật. Log: `interactive-shell-tests.log`.
- Bash syntax và go vet windows/amd64 + linux/amd64 + linux/arm64: PASS. Build lại ba tool và 11 suite cho mỗi target Linux: 28 checksum, **22 test binary**, ELF/SHA đối chiếu PASS. Audit: `dist/verification/interactive-audit.json`.
- Primary unit giữ nguyên byte; toàn bộ source packaging/lab so với hotfix trước không đổi; panel-reference working tree sạch. Không kết nối panel/VPS production trong phát triển.
- Linux core riêng trong gói chỉ phục vụ tiền kiểm offline, không được cài thành service. Cả hai archive official đã khớp digest metadata GitHub; executable hash được pin trong CLI. Tiền kiểm dùng private verified copy và confdir/môi trường riêng, không đọc config Xray hiện hữu.
- Gói amd64 `zic-xray-agent-installer-debian12-amd64-20261031-r1.tar.gz`: 47,459,119 byte; SHA256 `b37131aaad5d0941b9d026f0f100d6db780fc74c18444392fd0e59ffc6cdb864`.
- Gói arm64 `zic-xray-agent-installer-debian12-arm64-20261031-r1.tar.gz`: 42,651,209 byte; SHA256 `4ccedf20ae78e71b4af105319cd779cd4a8efe2c83de4d8ff4abc0bd058cffc1`.
- Giải nén cả hai gói: mỗi gói 35 file manifest khớp hash, 11 suite đúng target và script/unit LF; mock installer từ source đã giải nén PASS. Audit: `interactive-bundle-check.json`, `interactive-extracted-mocks.log`.

**Debian installer/systemd/TTY permissions execution: NOT RUN.** Linux/arm64 binary chỉ cross-build, chưa thực thi. Chưa có số đo VPS mới, không production-ready. Các test binary Linux và hướng dẫn một lệnh nằm trong [interactive-installer.md](interactive-installer.md).
