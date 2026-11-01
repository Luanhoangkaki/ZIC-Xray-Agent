package logging

import "errors"

// Code and Stage are closed technical identifiers. They never contain input,
// response bodies, paths or formatted underlying errors.
type Code uint8

const (
	Operation Code = iota
	Input
	AuthInput
	Auth
	Node
	Unsupported
	PanelTransport
	PanelStatus
	PanelRead
	PanelSize
	PanelResponse
	VLESSConfig
	TLSCertificate
	RealityConfig
	TLSRequired
	Policy
	CoreBinary
	CoreRead
	CoreVerify
	CoreCopy
	CoreConfig
	CoreExec
	CoreTimeout
	Storage
	Accounting
)

type Stage uint8

const (
	NoStage Stage = iota
	InstallArgs
	InstallStaging
	InstallCredentials
	InstallProbe
	InstallAdapter
	InstallVLESS
	InstallTLS
	InstallUsers
	InstallPolicy
	InstallCore
	InstallSave
)

type failure struct {
	stage Stage
	code  Code
	cause error
}

func (e *failure) Error() string {
	codes := [...]string{"OPERATION", "INPUT", "AUTH_INPUT", "AUTH", "NODE", "UNSUPPORTED", "PANEL_TRANSPORT", "PANEL_STATUS", "PANEL_READ", "PANEL_SIZE", "PANEL_RESPONSE", "VLESS_CONFIG", "TLS_CERTIFICATE", "REALITY_CONFIG", "TLS_REQUIRED", "POLICY", "CORE_BINARY", "CORE_READ", "CORE_VERIFY", "CORE_COPY", "CORE_CONFIG", "CORE_EXEC", "CORE_TIMEOUT", "STORAGE", "ACCOUNTING"}
	stages := [...]string{"", "INSTALL_ARGS_", "INSTALL_STAGING_", "INSTALL_CREDENTIALS_", "INSTALL_PROBE_", "INSTALL_ADAPTER_", "INSTALL_VLESS_", "INSTALL_TLS_", "INSTALL_USERS_", "INSTALL_POLICY_", "INSTALL_CORE_", "INSTALL_SAVE_"}
	if int(e.code) >= len(codes) || int(e.stage) >= len(stages) {
		return "E_OPERATION"
	}
	return "E_" + stages[e.stage] + codes[e.code]
}

func (e *failure) Unwrap() error { return e.cause }

// Failure retains a cause for errors.Is/As, but Error never formats that cause.
func Failure(code Code, cause error) error { return &failure{code: code, cause: cause} }

// At preserves a typed reason while adding the failing installation stage.
// Unknown causes use the caller's fixed fallback, never text matching.
func At(stage Stage, fallback Code, cause error) error {
	var typed *failure
	if errors.As(cause, &typed) {
		fallback = typed.code
	}
	return &failure{stage: stage, code: fallback, cause: cause}
}
