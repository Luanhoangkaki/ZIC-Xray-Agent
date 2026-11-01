# Điều tra E_OPERATION — v1.0.1-audit3-test

Bản sửa installer hiện dùng PATCH v1.0.1 và tag v1.0.1-audit3-test theo quy tắc docs/versioning.md. Candidate tại dist/release-candidates/v1.0.1-audit3-test/. Chủ repository đã cho phép phát hành thử nghiệm nếu tất cả gate local đạt; FAIL hoặc sai lệch phải dừng và báo cáo. Không sửa VPS Lab/production.

Các candidate v1.0.0-audit3-test, v1.0.0-audit3-test-r1 và bản đã phát hành v1.0.0-audit2-test giữ nguyên. Build dùng -buildvcs=false; BUILD.json ghi đúng commit nguồn, đối chiếu bằng rebuild độc lập checkout sạch. Không dùng version/suffix hoặc checksum cũ cho bản sửa mới.

## Nguyên nhân đã xác minh và giới hạn kết luận

1. `diagnostic.go` cũ phân loại bằng nguyên văn `err.Error()`. Hầu hết lỗi trong chuỗi cài đặt không có trong bảng, gồm nhánh từ chối VLESS plaintext với Panel ngoài loopback; vì vậy chỉ in `E_OPERATION`. Bản mới dùng kiểu lỗi chứa enum stage/reason, `errors.As` và `Unwrap` cho `errors.Is`; không phân loại bằng chuỗi lỗi. Lỗi không nhận diện vẫn bị che bằng mã cố định.
2. Hai installer archive audit2-test cũ đều ghi `validation/linux-*/xray` và agent là `0666`. Quy trình ZIP/NTFS/Windows tar không lưu execute bits Linux. Downloader cũ đặt `umask 077` và giải nén `--no-same-permissions`, nên trên Linux mode trở thành `0600`. Archive helper mới dùng GNU tar ghi executable/directory `0700`, data `0600`, không lấy mode từ NTFS; downloader đặt lại `0700` cho hai binary sau khi cả checksum archive và nội dung đã đạt. Không sửa binary Xray gốc; giữ pin official core.
3. `installcheck.Validate` không thực thi file Xray nguồn. Nó đọc/checksum, tạo bản sao riêng trong staging, xác minh lại rồi chạy `run -test` qua stdin; stdout/stderr core bị bỏ. Regression với official native core nguồn mode `0600` vẫn tiền kiểm thành công; nhánh quyền Unix thật chỉ chạy trên Linux. Vì vậy chỉ chmod nguồn không thể giải thích hoặc khắc phục mọi `E_OPERATION` sau đó. Bản sao mới còn được `Chmod(0700)` rõ ràng để không mất x bởi umask chặt hơn.

Không có response/config hay mã giai đoạn từ lần lỗi VPS cũ, nên **chưa thể xác định chính xác nhánh thất bại trên VPS đó**. Các kết luận trên chứng minh lỗi diagnostic/đóng gói và tái hiện các nhánh bằng dữ liệu giả; không gán nguyên nhân TLS hay API Key cho VPS khi thiếu bằng chứng. Không yêu cầu nhập lại API Key để điều tra; không thử token thật.

## Kiểm tra TLS không bị nới lỏng

Điều kiện vẫn là `spec.TLS == 0 && !panel.IsLoopbackPanel(auth.PanelURL)`. Nó kiểm tra URL Panel đã chuẩn hóa, không kiểm tra địa chỉ listen của node hoặc VPS. Chỉ IP loopback HTTPS dạng literal được nhận; tên miền (kể cả localhost hoặc tên miền resolve loopback) không được coi là loopback. HTTPS của Panel không cung cấp TLS cho kết nối VLESS.

| Config | Kết quả |
| --- | --- |
| TCP/raw VLESS, tls=0, flow rỗng, Panel IP loopback | Chỉ được tiếp tục tiền kiểm offline/mock; không bật proxy |
| TCP/raw VLESS, tls=0, Panel ngoài IP loopback | `E_INSTALL_TLS_TLS_REQUIRED`; giữ nguyên từ chối |
| tls=0 với Vision | `E_INSTALL_VLESS_VLESS_CONFIG`; không fallback |
| tls=1 với tls_settings hợp lệ theo schema | `E_INSTALL_VLESS_TLS_CERTIFICATE`; TLS chứng chỉ chưa được implement |
| tls=2 thiếu tls_settings | `E_INSTALL_PROBE_PANEL_RESPONSE`; schema không hợp lệ |
| tls=2 với key/SNI/short ID/target/flags không phù hợp | `E_INSTALL_VLESS_REALITY_CONFIG` |
| TCP/raw tls=2, REALITY hợp lệ, flow rỗng hoặc Vision | Tiếp tục users/policy/core/save; không chứng minh handshake hay nghiệm thu VPS |
| WS/gRPC/XHTTP/transport khác TCP, routes/settings/encryption ngoài subset | Từ chối trước core; không chuyển thành TCP/plaintext |

## Chuỗi tiền kiểm và mã cố định

| Bước | Mã/nhóm mã lỗi |
| --- | --- |
| Parse flags/selection | `E_INSTALL_ARGS_INPUT` |
| Kiểm tra staging và quyền private | `E_INSTALL_STAGING_STORAGE` |
| readCredentials: stdin/URL/Node ID hoặc token rỗng | `E_INSTALL_CREDENTIALS_INPUT`, `E_INSTALL_CREDENTIALS_AUTH_INPUT` |
| ProbeInstallation: network/HTTPS/redirect/timeout | `E_INSTALL_PROBE_PANEL_TRANSPORT` |
| Panel xác nhận auth thất bại hoặc node không tồn tại | `E_INSTALL_PROBE_AUTH`, `E_INSTALL_PROBE_NODE` |
| HTTP lỗi khác; đọc body lỗi; body quá lớn; schema/JSON sai | `E_INSTALL_PROBE_PANEL_STATUS`, `E_INSTALL_PROBE_PANEL_READ`, `E_INSTALL_PROBE_PANEL_SIZE`, `E_INSTALL_PROBE_PANEL_RESPONSE` |
| RequireAgentAdapter | `E_INSTALL_ADAPTER_UNSUPPORTED` |
| ParseVLESS và kiểm tra TLS | Xem bảng TLS |
| FetchUsers | `E_INSTALL_USERS_` với suffix `PANEL_TRANSPORT`, `AUTH`, `NODE`, `PANEL_STATUS`, `PANEL_READ`, `PANEL_SIZE`, `PANEL_RESPONSE` |
| RequireSupportedPolicies | `E_INSTALL_POLICY_POLICY` |
| installcheck.Validate | `E_INSTALL_CORE_CORE_BINARY`, `CORE_READ`, `CORE_VERIFY`, `CORE_COPY`, `CORE_CONFIG`, `CORE_EXEC`, `CORE_TIMEOUT` (cùng prefix `E_INSTALL_CORE_`) |
| saveNew | `E_INSTALL_SAVE_STORAGE` |

Chỉ auth 401/403 hoặc đúng literal HTTP 500 đã chứng minh từ source panel mới phân loại AUTH. HTTP 500 khác, lỗi DNS/HTTPS, body hỏng, TLS node hoặc lỗi file không bị coi là API Key sai. Literal message của panel là phần contract được nhận diện, không phải `err.Error()`; body không được in. Body HTTP 500 có JSON trùng key/không hợp lệ không được nhận diện như auth.

Không thêm prompt hoặc retry. Khi thất bại, giữ đúng một lần nhập bốn bước, in mã cố định và rollback; không cài/start service. Chỉ cần mã kỹ thuật để xác định bước tiếp theo, không cung cấp raw response, trace/debug log hoặc dữ liệu nhạy cảm.

## Kiểm thử và phần chưa chạy

Regression kiểm tra từng stage, Panel lỗi/JSON/schema hỏng, auth/node/user errors, transport/body limits, TLS/plaintext/REALITY, policy, official offline core, nguồn không executable, lỗi spawn/copy/cancel/rejected config, save conflict, stdout/stderr process chỉ có mã cố định, rollback và downloader/permission trên cả hai kiến trúc. Tất cả fixtures giả. Các kết quả cụ thể và checksum nằm trong `TEST_EXECUTION.json`, `PACKAGE_CHECK.json`, `SECURITY_SCAN.json`, `SOURCE_SECURITY_SCAN.json`, `SHA256SUMS` của candidate mới.

Windows native tests và Git Bash mocks không thay thực thi Linux, systemd, quyền Unix/noexec thật, reboot, RAM/CPU/FD/I/O/soak 10/20/30 kết nối trên VPS Debian 12. Các mục này tiếp tục NOT RUN. Observe mặc định, quản lý production và traffic sending vẫn bị khóa; không thay accounting/retry UNKNOWN hay panel source/API/database/Redis/queue. Không tuyên bố production-ready hoặc không mất byte tuyệt đối.
