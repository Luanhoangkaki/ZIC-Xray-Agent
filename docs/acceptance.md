# Nghiệm thu VPS 1 vCPU / 1 GB

Chưa chạy trên VPS. Test Go với panel giả lập không xác nhận khả năng vận hành node hoặc mức RAM/CPU.

Đã có bài chức năng với official core trên Windows loopback, gồm hai UUID và 10/20/30 kết nối, counter/ledger khớp. Kết quả và các giới hạn nằm trong [verification.md](verification.md); chưa đánh dấu PASS cho bất kỳ mục nghiệm thu Linux/VPS bên dưới.

## Công cụ thu CSV

`cmd/zic-bench` là công cụ Go riêng, không phải tiến trình con của agent và không được bật cùng service. Build script tạo binary cho hai kiến trúc. Trên VPS thử nghiệm đã được duyệt, truyền PID agent và Xray đang chạy:

```bash
./zic-bench -agent-pid "$AGENT_PID" -xray-pid "$XRAY_PID" -duration 30m -interval 1s > idle.csv
```

Collector dùng /proc và gọi `getconf CLK_TCK` một lần, tính CPU theo thời gian giữa mẫu; ghi riêng CPU/RSS của chính collector để thấy overhead. Nó dừng nếu PID biến mất hoặc starttime đổi, tránh ghép dữ liệu trước/sau restart. RSS từ /proc/PID/stat là số liệu kernel có thể xấp xỉ; khi cần đối chiếu, bổ sung smaps_rollup. Tổng CPU busy gồm steal, không gồm iowait. Tổng memory used = MemTotal - MemAvailable, không phải cộng RSS. Công cụ chưa thu PSS/cgroup/steal riêng hoặc tự tạo tải; phải bổ sung bằng chứng đó theo kế hoạch bên dưới. Chưa chạy collector trên Linux thật.

## Môi trường và phép đo

- Dùng VPS thử nghiệm được phê duyệt; ghi Debian/Ubuntu version, kernel, kiến trúc, số CPU, RAM/swap, bản agent/Xray chính thức, compiler, cấu hình chính sách, số user và thời gian thử. Thử riêng amd64/arm64 và cả hai hệ điều hành.
- Máy sinh tải đặt ngoài VPS. Đo CPU/RSS của agent, Xray, bộ sinh số đo riêng; ghi thêm MemAvailable, swap, tổng CPU, steal time và I/O toàn hệ thống. CPU tiến trình quy về 100% = một vCPU; lấy delta CPU-time chia elapsed wall-time, không chỉ đọc %CPU trung bình từ khi khởi động.
- Thu RSS từ /proc/PID/status, thêm PSS từ smaps_rollup nếu có. Ghi cả cgroup MemoryCurrent/Peak nhưng không coi là RSS. Chú ý PID/starttime thay đổi khi restart. Không gộp Xray vào chỉ số agent; đo idle có Xray và tách các cửa sổ đồng bộ.
- Lấy mẫu mỗi giây ở phép đo ngắn; giữ file CSV có timestamp, CPU%, RSS/PSS, PID/epoch và trạng thái tải. Chạy warm-up 5 phút, idle 30 phút, mỗi mức tải 10/20/30 kết nối ít nhất 15 phút; soak 24 giờ. Không ghi token, UUID hoặc private key vào bằng chứng.
- Báo mean/p95/peak theo tiến trình và hệ thống; kiểm tra RSS ổn định <20 MB (báo rõ dùng 20,000,000 byte), idle CPU trung bình <1% một core. Peak khởi động/đồng bộ phải báo riêng. Không coi GOMEMLIMIT hoặc MemoryMax là kết quả đo.

## Ma trận bắt buộc

1. Idle, 10/20/30 kết nối hoạt động; bao gồm truyền dữ liệu hai chiều, kết nối ngắn/dài, nhiều user và danh sách user lớn có số lượng công bố. So sánh lưu lượng cùng tầng đo counter Xray, không so trực tiếp byte ứng dụng với byte mã hóa ngoài đường truyền.
2. ETag 200/304, cấu hình thay đổi, thu hồi user/quota, interval panel thay đổi; không nới chính sách lúc panel offline.
3. DNS/TLS lỗi, timeout, 401/403/404/422/429/5xx, JSON sai/quá lớn, redirect, token có ký tự đặc biệt; không tạo cấu hình đích hoặc chạy node khi tiền kiểm lỗi. Xác minh quyền 0700/0600 thật trên Linux.
4. Panel offline dài, mất phản hồi sau khi panel đã ghi batch, restart agent giữa checkpoint/send/ack, mất điện ở các điểm fsync, đĩa đầy, ledger hỏng. Tổng batch chỉ được tính một lần hoặc hệ thống phải báo trạng thái chưa thể đối soát, không tự đánh dấu thành công.
5. Restart Xray có kiểm soát, SIGKILL Xray, counter reset/xóa/tạo lại và reboot VPS khi đang truyền. Kiểm chứng nguồn durable bổ sung; nếu chỉ có counter RAM thì tiêu chí không mất traffic chưa đạt.
6. systemd stop/start/restart, SIGTERM, backoff khi crash, resource limit và đầy log; singleton agent, không worker leak, không mất batch do rotation/compaction.

Không ghi PASS cho trường hợp chưa chạy. Không bật node production trước khi có kết quả và phê duyệt. Panel phải được giữ nguyên; dùng UNKNOWN/đối soát thủ công và công bố rủi ro, không coi phản hồi hiện tại là dedup/receipt. Xem traffic-policy.md và bộ khởi đầu thử nghiệm Debian 12 trong debian12-lab.md.
