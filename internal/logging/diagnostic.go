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
	var fixed *failure
	if errors.As(err, &fixed) {
		return fixed.Error()
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
	return "E_OPERATION: thao tác kỹ thuật thất bại; kiểm tra cấu hình, quyền file và mã lỗi cố định của Agent."
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
