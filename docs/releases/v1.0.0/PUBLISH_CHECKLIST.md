# v1.0.0: hồ sơ để chủ repository duyệt

Repository đích: https://github.com/Luanhoangkaki/ZIC-Xray-Agent

Đợt chuẩn bị 2026-11-01 chỉ dành cho tag thử `v1.0.0-audit2-test`, Pre-release TEST ONLY / OBSERVE MODE / NOT PRODUCTION READY. Dùng bộ mới `dist/release-candidates/v1.0.0-audit2-test/`, không dùng `audit2` cũ hoặc installer `20261031-r1`. `COMMIT_FILES.txt` là allowlist source diff đã review; `UPLOAD_FILES.txt` là danh sách asset cụ thể. Chưa stage/commit/push/tag/publish. Không chạy lệnh GitHub bên dưới trong phiên chuẩn bị này; việc chạy cần chủ repository duyệt bộ cụ thể.

**HOLD sau audit privacy/resources:** áp dụng [điều kiện phát hành cập nhật](RELEASE_GATE.md). Các archive review cũ chưa chứa audit fixes. Không push/tag/draft/publish/upload trước khi nghiệm thu Linux/VPS thực tế đạt và chủ repository xác nhận bộ source/asset cuối. Các lệnh tham khảo bên dưới không phải quyền thực thi.

**Không có push, tag, GitHub draft/release hay asset upload nào đã được thực hiện.** Đây chỉ là file và lệnh tham khảo để chủ repository tự quyết định sau review.

1. Đọc RELEASE_NOTES.md, TEST_RESULTS.md và SECURITY_SCAN.json. Giữ NOT RUN cho mọi bài chưa thực thi, đặc biệt Linux-specific và systemd Debian 12; không đổi nhãn thành production-ready.
2. Kiểm `sha256sum --check --strict SHA256SUMS` trong thư mục review. Đối chiếu ASSETS.json với đúng sáu asset tải xuống: hai archive, hai sidecar, downloader và sidecar của downloader. Aggregate SHA256SUMS là asset bổ sung; không dùng wildcard để upload toàn bộ evidence/cache.
3. Đối chiếu SOURCE_FILES.sha256 với source được review. Đây là hash byte working-tree, gồm cả LF/CRLF, không phải Git blob/commit hash. Workspace hiện có commit nền `428a0fa`; source audit còn trong working tree. Phải kiểm trạng thái repository đích bằng thao tác chỉ đọc tại thời điểm chuẩn bị push, không suy ra remote vẫn trống từ metadata cũ. Trước khi tạo tag, source phải nằm trong commit thật đã được duyệt và xác nhận lại hash; nếu checkout đổi line ending thì phải tái tạo source/manifest/build để đối chiếu, không bỏ qua mismatch. Không dùng sourceTree hash thay cho Git commit SHA. Nếu thay code/rebuild artifact, phải chạy kiểm thử/quét lại và sinh checksum/downloader mới; không thay byte sau khi đã duyệt. Chỉ thực hiện push/tag sau các điều kiện và xác nhận trong RELEASE_GATE.md.
4. Chạy test Linux trên máy riêng, không production/Lab active: từ source đã kiểm tra, dùng official core riêng, chạy `go test -count=1 -v ./...`, rootless scripts/test-installer.sh và scripts/test-lab-startup.sh trên Linux. Host build có thể khác máy đo; đừng bắt VPS 1 GB phải build compiler/dependency. Có thể chuyển 11 test binary đúng target từ dist/lab-tests cùng core riêng để chạy, nhưng test binary/fixture không nằm trong gói installer công khai.
5. Trên Debian 12 riêng được phép, kiểm unit bằng systemd-analyze, thực hiện TTY visible-input, cài mới/start/stop/restart/reboot, mode/quyền 0700/0600, update và rollback. Giữ config/ledger/evidence; không sửa panel hoặc gửi POST thật. Đây chưa phải số đo VPS 1 vCPU/1 GB, 10/20/30 kết nối hay soak.
6. Chủ repository tự kiểm tra quyền sở hữu nguồn và license dự án; không có license dự án nào được tự chọn trong lần chuẩn bị. License dependency/core được đóng gói riêng. Không đưa credentials/backup/config/runtime của máy test lên GitHub.
7. Chỉ sau khi chủ repository chủ động push source và tạo tag **v1.0.0-audit2-test trỏ đúng commit đã duyệt**, có thể dùng lệnh tham khảo dưới để tạo **Draft + Pre-release**, rồi upload đúng asset. Phiên chuẩn bị này không chạy lệnh đó. `--verify-tag` tránh GitHub CLI tự tạo tag khi chưa tồn tại:

```bash
gh release create v1.0.0-audit2-test --repo Luanhoangkaki/ZIC-Xray-Agent --verify-tag --draft --prerelease --title 'v1.0.0-audit2-test - TEST ONLY observer' --notes-file RELEASE_NOTES.md zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz zic-xray-agent-installer-debian12-amd64-v1.0.0.tar.gz.sha256 zic-xray-agent-installer-debian12-arm64-v1.0.0.tar.gz zic-xray-agent-installer-debian12-arm64-v1.0.0.tar.gz.sha256 get-zic-xray-agent-v1.0.0.sh get-zic-xray-agent-v1.0.0.sh.sha256 SHA256SUMS
```

Chưa publish tự động. Khi chủ repository tự publish sau review, kiểm URL cố định và tải lại đối chiếu SHA; không đổi sang latest. Test executable, runtime test data, private VM/SSH key, certificate, .tools, .cache, panel-reference, receipt và backup không phải release asset. Source test được review/commit như mã nguồn, không trộn vào installer archive.
