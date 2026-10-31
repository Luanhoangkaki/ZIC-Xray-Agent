# V2B-ZIC: bằng chứng API được sử dụng

Kiểm tra ngày **2026-10-30, Asia/Saigon**, bằng clone đầy đủ repository [kutycma/v2b-zic](https://github.com/kutycma/v2b-zic) vào `panel-reference/`. Source PHP chỉ được đọc, không chỉnh sửa hay chạy/triển khai.

## Commit và so sánh

- `f7784009` tồn tại và được resolve thành `f778400907da4c47f0466abcab21030009e2770e`.
- `HEAD`, `origin/master`, `origin/HEAD` đều trỏ đến SHA này ở lần clone. Đây là bản mới nhất của nhánh mặc định khi kiểm tra, không phải khẳng định repo sẽ giữ nguyên sau đó.
- `origin/dev` trỏ đến `69c9a3ace681ba10f671e793abc096bd9dfd5da1`, commit khác và tree khác. Đã chạy diff bốn file theo yêu cầu: không có khác biệt với production. Các file khác trong diff thuộc payment/Telegram/update-dev; không suy luận chỉ từ tên branch hoặc thời gian commit.
- `git status --short` của reference trả rỗng sau phân tích. Không checkout đè lên dự án agent; reference được loại khỏi gói binary và không là dependency Go.

Lệnh đọc metadata có thể cần `git -c safe.directory=C:/ZIC-Xray-Agent/panel-reference -C panel-reference ...` trên máy Windows có user sandbox khác owner clone. Không thay cấu hình Git toàn hệ thống.

SHA256 các file trong **working tree Windows CRLF** tại production/default HEAD (Git báo `i/lf w/crlf`). Đây là hash byte của bản checkout đã đọc, không phải Git blob hash; checkout LF trên Linux sẽ có SHA256 khác. SHA commit và Git diff ở trên là bằng chứng so sánh phiên bản độc lập với EOL.

| File | SHA256 |
| --- | --- |
| `app/Http/Routes/V1/ServerRoute.php` | `624ca0ae92adc64f3e84a1bbdabbbc006614821439bc5b5bed37f895b805a8b3` |
| `app/Http/Controllers/V1/Server/UniProxyController.php` | `594185d1d34b8cdf3afba221be871e02699134a253f17af5d964e0288ccaaf94` |
| `app/Services/ServerService.php` | `e87aad68a650ea313bdc0465e5ec397a237c0d7a69762b173dfd69c028eea2cf` |
| `app/Services/UserService.php` | `f45888a2c74329602fa400c6c7b1708b55923cda2baeed90d40e17e6e80d3e35` |

## Xác thực và endpoint

[Route tại production](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Http/Routes/V1/ServerRoute.php) đăng ký `server/{class}/{action}`; `RouteServiceProvider` gắn prefix `/api/v1`. Authentication nằm trong constructor của [UniProxyController](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Http/Controllers/V1/Server/UniProxyController.php), không phải middleware `Server` của Xboard đã tham khảo trước đây.

Agent dùng `GET /api/v1/server/UniProxy/config` và `GET /api/v1/server/UniProxy/user`, với `token`, `node_id`, `node_type=vless` trong query HTTPS. Constructor so sánh token với `config('v2board.server_token')`, gọi `ServerService::getServer`; nhánh `vless` chọn `ServerVless::find(node_id)`.

Các thông báo nguồn xác định được: token rỗng/sai và node không tồn tại đều abort HTTP 500. Exception handler có thể trả JSON `message`. Adapter chỉ nhận diện đúng ba literal đã thấy trong source, tự tạo thông báo an toàn; không in body bất kỳ, trace, token hoặc URL. Lỗi 500 khác không được suy thành token sai. Redirect bị từ chối, không chuyển token đến host khác.

Token nằm trong query là đường truyền đã xác minh, không phải argv của tiến trình. Access log của panel/reverse proxy phải được quản trị riêng để che query; agent không có quyền đảm bảo hoặc sửa log bên panel. Header Bearer không được source constructor sử dụng làm token. Không cấu hình request MessagePack; agent yêu cầu JSON.

## Config VLESS và REALITY

Nhánh `config` của controller trả `server_port`, `network`, **`networkSettings`** từ model `network_settings`, `tls`, `flow`, `tls_settings`, `encryption`, `encryption_settings`, `base_config`. Nếu node có route ID, thêm `routes`. Không có `protocol` hoặc `listen_ip` trong phản hồi này. `base_config` lấy hai interval panel, mặc định 60. Cả config và user đều có ETag và nhánh 304.

Theo [admin VlessController](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Http/Controllers/V1/Admin/Server/VlessController.php), TLS có 0/1/2; REALITY là 2. Admin sinh `private_key`, `public_key`, `short_id` nếu thiếu và đặt `server_port` mục tiêu mặc định chuỗi `443`. `flow` chỉ nhận Vision và bị xóa khi network khác TCP. Model cast `tls_settings`, `network_settings`, `encryption_settings` thành array.

UI admin tại cùng commit có các trường `server_name`, `dest`, `server_port`, `xver`, key, short ID; mô tả `dest` mặc định theo SNI. Adapter hiện ánh xạ một SNI, đích TCP, xver 0/1/2, private key X25519 32 byte, short ID hex đệm tới 8 byte. Public key nếu có phải khớp private key. Không đoán khóa/đích khi thông tin bắt buộc sai.

Phạm vi primitive hiện tại: TCP VLESS không TLS chỉ trên loopback phục vụ test; TCP REALITY/Vision. Không tự hạ TLS chứng chỉ thành plaintext, không đổi WS/gRPC/XHTTP thành TCP, không bỏ route, encryption/PQ hoặc network settings chưa hỗ trợ. Các trường chưa biết đánh dấu chưa hỗ trợ; schema validation cho snapshot không đồng nghĩa khả năng quản lý node.

## Danh sách người dùng và quyền truy cập

[ServerService::getAvailableUsers](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Services/ServerService.php) lọc group của node, `u+d < transfer_enable`, chưa hết hạn (hoặc null), banned=0. Select chỉ gồm `id`, `uuid`, `speed_limit`, `device_limit`. Controller bỏ trường null rồi bọc trong `{"users":[...]}`. Một mảng rỗng hợp lệ nghĩa là không có user; thiếu/null/object không được coi tương đương mảng rỗng để thu hồi nhầm user.

Không có expiry timestamp hoặc remaining quota trong payload này. Chưa có bằng chứng để tự suy ra thời hạn/byte còn lại khi panel offline. Agent parse giới hạn, không bỏ qua trường lạ. Khi speed/device limit >0, phần quản lý không được phép áp dụng user như thể không có giới hạn. Hiện chưa có enforcement; daemon chỉ observe.

Email Stats nội bộ được tạo từ Node ID, user ID và hash thế hệ UUID; không phải email khách hàng. UUID đổi không dùng chung danh tính counter cũ. User bị panel loại khỏi danh sách vẫn cần giữ ledger chưa đối soát. API RemoveUser xác minh danh sách mới, nhưng không có cam kết các kết nối đã xác thực trước đó bị cắt ngay; đó là một tiêu chí kiểm thử lifecycle còn thiếu.

## Push traffic: payload đã xác minh, biên nhận còn thiếu

Wire shape của `push` là JSON object với key user ID và value `[upload_bytes, download_bytes]`, ví dụ dữ liệu giả `{"42":[100,200]}`. Không tự bọc `batch`, không tự gửi user UUID thay ID và không tự nhân rate. `TrafficFetchJob` tăng Redis upload/download và nhân `server['rate']`; các job StatUser/StatServer xử lý thống kê riêng.

[UserService::trafficFetch](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Services/UserService.php) dispatch TrafficFetchJob, StatUserJob và StatServerJob. Queue driver quyết định thực thi sync/async; chưa đọc cấu hình runtime production nên không khẳng định driver. Controller chỉ trả `{"data":true}`. Đây không phải receipt có batch ID gắn với việc cộng traffic bền vững.

Trong đường đi này không có batch dedup, receipt lookup hoặc transaction chung giữa ba job và response. [TrafficFetchJob](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Jobs/TrafficFetchJob.php) dùng các lệnh increment và cho thử lại job, không có khóa chống lặp từng batch. Mất response sau xử lý, crash giữa các increment hoặc queue retry có thể gây trạng thái không xác định/đếm lặp; agent không thể sửa bằng WAL đơn phương.

Do đó **không có POST traffic trong daemon**. Agent đã có outbox và sender thư viện chỉ thử trên HTTPS loopback: SENDING được fsync trước mạng, HTTP data=true chỉ là ACCEPTED, mọi kết quả không rõ thành UNKNOWN và không tự retry. Không thêm trường/header/API hoặc sửa panel, DB, Redis hay queue. Đối soát thủ công giữ evidence và không tự requeue gói cũ; xem [traffic-policy.md](traffic-policy.md).

Kết luận về schema dựa trên source đã pin, không là kết quả E2E với một PHP panel đang chạy. Các fixture HTTP được suy ra rõ ràng từ source, không giả là response production đã chụp.

## Installer bốn lựa chọn: đối chiếu bổ sung 2026-10-31

Menu 1/2/3/4 query UniProxy với `node_type=shadowsocks/trojan/hysteria/vless` tương ứng; ServerService::getServer chọn model ServerShadowsocks/ServerTrojan/ServerHysteria/ServerVless. Không dùng `node_type=v2node` hoặc API riêng của V2Node chỉ vì UI giống V2Node.

Constructor alias `hysteria2` thành `hysteria` để tìm model, còn nhánh config trả `version` và phân nhánh version 1/2: v1 dùng obfs_password làm obfs; v2 thêm ignore_client_bandwidth, obfs và obfs-password. HysteriaController::save validate version in:1,2; model không cast version nên probe nhận integer 1/2 hoặc chuỗi chính xác "1"/"2", không suy đoán khi thiếu. Alias model không phải bằng chứng giao thức hai version tương đương.

Probe cài đặt cho phép đọc config đúng bốn family; runtime Credentials.Validate vẫn VLESS-only. Shadowsocks/Trojan/Hysteria chưa có adapter agent được kiểm chứng nên bị từ chối trước lưu credential/cài/start. VLESS phải qua schema, policy và official core offline test. Xem [interactive-installer.md](interactive-installer.md) cho ma trận hỗ trợ và cách chạy.
