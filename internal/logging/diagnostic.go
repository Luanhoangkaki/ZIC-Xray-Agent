package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
)

// SuppressUnstructured is called once at process entry, before credentials or
// network input are read. App technical events still use Logger.Event. Library
// loggers must not bypass that fixed-code boundary with URLs/peer identities.
func SuppressUnstructured() {
	log.SetOutput(io.Discard)
}

// Diagnostic emits only exact trusted messages/codes. Unknown/wrapped errors
// are never rendered: PathError, network and JSON errors can contain input.
func Diagnostic(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "E_CANCELLED: thao tác đã dừng."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "E_TIMEOUT: thao tác kỹ thuật quá hạn."
	}
	if errors.Is(err, os.ErrPermission) {
		return "E_ACCESS: không đủ quyền truy cập file kỹ thuật."
	}
	if errors.Is(err, os.ErrNotExist) {
		return "E_CONFIG_FILE: thiếu file cấu hình hoặc binary cần thiết."
	}
	switch err.Error() {
	case "lựa chọn phải là một số từ 1 đến 4", "Node ID phải là số nguyên dương":
		return "E_INPUT: lựa chọn phải trong 1–4 và Node ID phải là số nguyên dương."
	case "Server Token trống hoặc không hợp lệ":
		return "E_AUTH_INPUT: API Key trống hoặc không hợp lệ."
	case "Server Token bị từ chối bởi V2B-ZIC (HTTP 500)", "Server Token bị từ chối hoặc không có quyền truy cập node (HTTP 401)", "Server Token bị từ chối hoặc không có quyền truy cập node (HTTP 403)":
		return "E_AUTH: panel từ chối API Key hoặc quyền truy cập."
	case "Node ID không tồn tại cho Node Type đã chọn (HTTP 500)":
		return "E_NODE: node không tồn tại cho giao thức đã chọn."
	case "Shadowsocks: agent chưa có adapter được kiểm chứng; không cài hoặc khởi động":
		return "E_UNSUPPORTED: adapter Shadowsocks chưa được kiểm chứng; chưa cài."
	case "Trojan: agent chưa có adapter được kiểm chứng; không cài hoặc khởi động":
		return "E_UNSUPPORTED: adapter Trojan chưa được kiểm chứng; chưa cài."
	case "Hysteria version 1: Xray được kiểm chứng chỉ nhận version 2; không chuyển đổi sang Hysteria2":
		return "E_UNSUPPORTED: Hysteria1 không được core đã pin hỗ trợ; không đổi sang Hysteria2."
	case "Hysteria2 (panel version 2): agent chưa có adapter được kiểm chứng; không cài hoặc khởi động":
		return "E_UNSUPPORTED: adapter Hysteria2 chưa được kiểm chứng; chưa cài."
	case "Xray không khớp binary chính thức v26.10.29 đã kiểm chứng; không tự chấp nhận core khác":
		return "E_CORE_VERIFY: binary core không khớp phiên bản/checksum đã kiểm chứng."
	case "Xray từ chối cấu hình hoặc tiền kiểm quá hạn; chưa cài Agent":
		return "E_CORE_CONFIG: tiền kiểm core thất bại; chưa cài Agent."
	case "speed_limit/device_limit requires an enforcement mechanism; refusing to bypass policy":
		return "E_POLICY: chính sách giới hạn chưa được triển khai; không được bỏ qua."
	case "durable state write failed; stop and reload before continuing":
		return "E_STORAGE: ghi trạng thái bền vững thất bại; cần kiểm tra ổ đĩa/quyền file."
	case "traffic checkpoint/reset/epoch anomaly; reconciliation required; existing ledger retained":
		return "E_ACCOUNTING: cần đối soát checkpoint; trạng thái cũ được giữ nguyên."
	default:
		return "E_OPERATION: thao tác kỹ thuật thất bại; kiểm tra cấu hình, quyền file và mã lỗi cố định của Agent."
	}
}

// RecoverMain does not print the panic value or a stack with argument values.
// Runtime-fatal/background faults still require the per-unit stderr/core-dump
// controls and host validation; recovery here is not an absolute guarantee.
func RecoverMain() {
	if recover() != nil {
		fmt.Fprintln(os.Stderr, "E_INTERNAL: lỗi nội bộ; tiến trình dừng an toàn để phục hồi.")
		os.Exit(1)
	}
}
