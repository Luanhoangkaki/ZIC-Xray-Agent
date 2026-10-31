# Thiết kế bắt buộc trước khi bật daemon

Tình trạng cập nhật: daemon mặc định `observe`; chỉ mode opt-in `manage-test` trên loopback thực hiện mutation. Đã có snapshot/ledger/outbox checksum và atomic rename, singleton OS lock, SIGTERM, lịch Stats/queue độc lập backoff API, gRPC controller và unit systemd. Daemon không POST traffic ở bất kỳ mode nào; production manage vẫn bị chặn. Sender thư viện chỉ dùng trong test loopback và ghi UNKNOWN khi không xác định kết quả. Xem [biên bản test](verification.md), [source panel](panel-api.md) và [chính sách traffic](traffic-policy.md).

## Ngân sách và vòng đời

Một tiến trình Go, một lịch đồng bộ mặc định 60 giây; chỉ đổi theo số nguyên dương được kiểm tra từ panel. Không polling liên tục; không tạo goroutine trên mỗi user. Dùng một HTTP transport giữ kết nối và một client gRPC Stats tái sử dụng. HTTP/API timeout có giới hạn; không chạy hai lượt cùng lúc; mọi goroutine phải có context hủy. Giới hạn kích thước phản hồi, số user, số batch, dung lượng đĩa và hàng chờ theo khả năng đo được; vượt ngưỡng phải báo lỗi và giữ chính sách an toàn, không âm thầm cắt user.

Agent không giữ lịch sử traffic, JSON thô hay bản sao danh sách user trong RAM nếu không cần. Thông tin xác thực tối thiểu và bảng ánh xạ user vẫn cần thiết cho tính đúng đắn. Không hạ bảo mật TLS, bỏ xác thực, bỏ user bị thu hồi hoặc nới quota để đạt mục tiêu RAM. Đo binary thật cùng đầy đủ dependency và tải thực; không suy ra RSS từ kích thước binary hay Go heap.

Xray chính thức là dịch vụ riêng; không nhúng core trong agent, không sử dụng V2Node. Mọi thay đổi Xray cần được phê duyệt. Khi thiếu Stats API, báo rõ yêu cầu cấu hình để duyệt, không tự bật.

## Giới hạn tính chính xác của lưu lượng

[Stats API chính thức](https://github.com/XTLS/Xray-core/blob/main/app/stats/command/command.proto) có cờ `reset`; [implementation](https://github.com/XTLS/Xray-core/blob/main/app/stats/command/command.go) đặt counter về 0 ngay trong yêu cầu và trả về giá trị. Không có giao dịch với việc ghi đĩa của agent. [Counter](https://github.com/XTLS/Xray-core/blob/main/app/stats/counter.go) là giá trị atomic trong RAM.

Vì vậy không dùng reset=true để giả định lấy-và-ghi bền vững: crash sau reset trước fsync có thể làm mất mẫu. Đọc reset=false và lưu mốc trước giúp agent restart khi Xray còn chạy, nhưng không khôi phục được byte phát sinh sau lần đọc cuối nếu Xray crash hoặc counter bị xóa/reset. Không được tự đặt chênh lệch âm thành 0 rồi tuyên bố không mất traffic. Đổi epoch và cảnh báo sai lệch cần minh bạch; phát hiện reset bằng counter giảm đơn thuần không đủ vì counter mới có thể đã vượt mốc cũ.

Đã triển khai nhận diện epoch core, đọc counter không reset, ghi baseline/pending trong một checkpoint và chuyển pending sang outbox bằng một checkpoint riêng atomic. Batch có identity/epoch/sequence/ID/digest; payload chỉ gộp khi chưa từng thử. Fsync SENDING trước mạng; crash chuyển UNKNOWN, tuyệt đối không phát lại tự động. Không tự xóa batch hoặc compact evidence để vượt giới hạn đĩa. Không có biên nhận billing durable; ACCEPTED không được coi là proof đã cộng lưu lượng chính xác.

[UniProxy V1 của V2B-ZIC production](https://github.com/kutycma/v2b-zic/blob/f778400907da4c47f0466abcab21030009e2770e/app/Http/Controllers/V1/Server/UniProxyController.php) dispatch các job traffic/stat qua UserService rồi trả data=true; không có receipt/idempotency key. Queue driver runtime chưa được kiểm chứng. Do đó khi panel đã xử lý nhưng phản hồi bị mất, client không thể biết nên gửi lại hay không; retries từng job cũng cần chống lặp bên panel. Một WAL riêng của agent không giải quyết được tình huống này. Không bật tự động gửi lại mù hoặc tuyên bố exactly-once.

Cam kết tuyệt đối không thể suy ra từ giao thức hiện tại. Thay đổi panel/source/API/DB/Redis/queue bị cấm và không nằm trong kế hoạch triển khai. Chính sách được chọn là UNKNOWN + đối soát thủ công, giữ evidence, không auto retry và không tuyên bố tuyệt đối lossless. Khi core reboot, giữ pending cũ và incident về tail không biết, thu counter core mới từ zero. Graceful shutdown lấy mẫu cuối và tắt inbound thử nếu có thể, nhưng không chứng minh session drain hoặc chống mất điện.

## Bảo mật bộ cài

Nhập đúng thứ tự URL, token ẩn, Node ID; mặc định VLESS. HTTPS bắt buộc, không theo redirect, không truyền token trong argv/env, không ghi request URL hoặc response body vào log. Token query là ràng buộc giao thức UniProxy V1 chuẩn, cần che query ở panel/proxy. Không lấy cấu hình mặc định thay thế khi panel sai hoặc không phản hồi. Snapshot panel không có nghĩa là cấu hình Xray đã sẵn sàng.

Hiện bộ cài xác minh config/user rồi cài binary và unit observer tắt mặc định; không hỗ trợ ghi đè/upgrade, không enable/start và không khởi động node. Daemon đã có singleton/checkpoint, nhưng transaction nâng cấp và rollback node còn thiếu. Unit dùng DynamicUser, LoadCredential và StateDirectory riêng; nguồn token vẫn root 0600. Không cấp quyền sửa file Xray cho observer.

Ledger v2 có baseline/pending/outbox/incident; không có batch acknowledgement bền vững từ panel. Lần mẫu đầu chỉ lập baseline; byte trước đó không tự tính là traffic mới. PID file phải đúng core của API, boot ID/PID/starttime được lấy trước/sau Stats RPC. Client khác reset counter vẫn có thể gây reset không phát hiện nếu số mới kịp vượt mốc cũ. Mọi giới hạn còn lại được công bố trong traffic-policy.md; không che bằng thay đổi panel hoặc bỏ kiểm tra xác thực.
