# Phạm vi client API Xray

Agent chỉ liên lạc với một core chính thức đang chạy ngoài tiến trình. Không import module `github.com/xtls/xray-core`, không khởi động core từ daemon, không dùng V2Node. Endpoint gRPC hiện chỉ cho `127.0.0.1:port` hoặc `[::1]:port`, không DNS/public IP. Core phải được operator cấu hình API/policy Stats đúng và phê duyệt trước; agent không tự thay file Xray.

`internal/xrayapi` dùng gRPC/protobuf, một connection tái sử dụng, timeout mỗi RPC 5 giây, giới hạn send/receive 4 MiB; không cấu hình retry cho mutation. Lỗi trả ra ngoài được làm sạch, không chứa gRPC response tùy ý có thể lộ user/key.

Descriptor tối thiểu có wire field numbers từ source Xray chính thức; tên local descriptor không gửi trên wire. `TypedMessage.type` dùng tên canonical. Các nguồn đã đọc:

- [HandlerService](https://github.com/XTLS/Xray-core/blob/main/app/proxyman/command/command.proto), [typed message](https://github.com/XTLS/Xray-core/blob/main/common/serial/typed_message.proto), [User](https://github.com/XTLS/Xray-core/blob/main/common/protocol/user.proto), [VLESS Account](https://github.com/XTLS/Xray-core/blob/main/proxy/vless/account.proto).
- [StatsService](https://github.com/XTLS/Xray-core/blob/main/app/stats/command/command.proto): chỉ QueryStats reset=false, lọc chính xác namespace của node và uplink/downlink; không dùng online-user list thay cho toàn bộ counters.
- [Inbound config](https://github.com/XTLS/Xray-core/blob/main/core/config.proto), [receiver](https://github.com/XTLS/Xray-core/blob/main/app/proxyman/config.proto), [stream](https://github.com/XTLS/Xray-core/blob/main/transport/internet/config.proto), [REALITY](https://github.com/XTLS/Xray-core/blob/main/transport/internet/reality/config.proto), [VLESS inbound](https://github.com/XTLS/Xray-core/blob/main/proxy/vless/inbound/config.proto), address/port proto.

Các link main phục vụ đối chiếu nguồn; tương thích binary thực tế chỉ đã kiểm tra với **official v26.10.29 / 0afec66, Windows amd64**. Không suy rằng mọi phiên bản Xray đều hỗ trợ GetInboundUsers hoặc mọi field. Bản thiếu API cần báo lỗi thay vì tự coi như đã đồng bộ.

Primitive hiện có: AddInbound TCP/REALITY, RemoveInbound (tag quản lý), AddUser/RemoveUser, GetInboundUsers, ListInbounds/Inspect và ReconcileUsers. Fingerprint dùng protobuf xác định để cùng cấu hình không gây recreate ngẫu nhiên. Controller manage-test ghi intent trước mutation, xác minh ownership/hash/epoch qua core, tiếp tục sau crash theo state quan sát được và xác nhận user cuối. Reconcile xóa trước thêm, checkpoint trước xóa; không rollback về UUID đã thu hồi. Không tiếp quản inbound lạ/drift. Đây là chuỗi thao tác phục hồi được trong lab, không phải transaction server atomic; cắt các session đang hoạt động, full REALITY handshake và Linux lifecycle vẫn cần nghiệm thu.

Bài test thật tạo **một core tạm** trên loopback và dùng chính core đó làm client/server cho hai tài khoản test, cùng echo server Go. Để echo đến loopback, fixture khai báo `finalRules` cho phép đúng `127.0.0.1/32`; source Freedom và log thực tế xác nhận v26.10.29 mặc định chặn đích private cho VLESS. Quy tắc cho phép này **chỉ có trong fixture**, không được ghi vào cấu hình Xray của người dùng.

API tạo inbound REALITY và user Vision đã được core thật chấp nhận. Chưa kiểm thử handshake REALITY, tương tác thiết bị thật, TLS certificate hoặc transport khác. Unit đã giới hạn tài nguyên không có nghĩa RSS/CPU đạt mục tiêu.
