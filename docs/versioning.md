# Quy tắc Semantic Versioning

Áp dụng từ bản sửa installer `v1.0.1-audit3-test`, theo yêu cầu chủ repository ngày 2026-11-01.

- Sửa lỗi: tăng PATCH, ví dụ `1.0.0` → `1.0.1`, lần sửa lỗi sau `1.0.1` → `1.0.2`.
- Tính năng mới tương thích ngược: tăng MINOR và đặt PATCH về 0, ví dụ `1.0.1` → `1.1.0`.
- Thay đổi không tương thích: tăng MAJOR và đặt MINOR/PATCH về 0, ví dụ `1.1.0` → `2.0.0`.
- Suffix prerelease chỉ nhận diện vòng kiểm thử. Đổi `audit2` sang `audit3` hoặc `r1` không thay cho việc tăng PATCH khi sửa lỗi.
- Không đổi version, tag hoặc asset của release đã phát hành. Không force-push tag, ghi đè asset hoặc tái sử dụng checksum cũ cho byte mới.

`internal/buildinfo/version.go` là nguồn version của agent, CLI `version` và HTTP User-Agent. BUILD.json, manifest, installer, tên archive/downloader, URL/tag ghim, checksum và tài liệu release phải thống nhất với nguồn đó. Version Xray-core, protocol, API và durable-state schema độc lập; không tăng chúng bằng thay thế chuỗi hàng loạt.

Vòng hiện tại dùng tag `v1.0.1-audit3-test`, archive base `v1.0.1` và downloader `get-zic-xray-agent-v1.0.1.sh`. Bộ chuẩn bị `v1.0.0-audit3-test-r1` được giữ để đối chiếu, không phát hành dưới tên cũ. Bản đã phát hành `v1.0.0-audit2-test` giữ nguyên.

Mọi bản mới phải rebuild đúng source, có kiểm chứng source–binary độc lập, test/secret-pattern scan/package verification và SHA256 mới. Chỉ công bố lệnh cài sau khi tải lại asset công khai và kiểm hash. FAIL hoặc sai lệch ở release gate phải dừng phát hành và báo cáo; các bài Linux/systemd/VPS chưa chạy phải tiếp tục ghi NOT RUN. Version mới không cấp quyền thay đổi Lab/production.
