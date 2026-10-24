package pluginapi

type contractError string

func (e contractError) Error() string { return string(e) }

const (
	ErrInvalidManifest     contractError = "invalid plugin manifest"
	ErrUnsupportedProtocol contractError = "unsupported protocol version"
	ErrUnknownAction       contractError = "unknown action"
	ErrInvalidInput        contractError = "invalid job input"
	ErrUndeclaredOutcome   contractError = "undeclared outcome"
	ErrJobNotFound         contractError = "job not found"
	ErrJobConflict         contractError = "job id reused with a different request"
	ErrJobNotActive        contractError = "job is not active"
	ErrUnsupported         contractError = "capability not supported by action"
	ErrUnauthorized        contractError = "unauthorized"
	ErrPayloadTooLarge     contractError = "payload too large"
	ErrUpstream            contractError = "upstream failure"
	ErrInvalidSchema       contractError = "invalid json schema"
	ErrSchemaViolation     contractError = "document does not match schema"
)

const ProtocolVersion = "1"

const (
	PathManifest    = "/v1/manifest"
	PathHealth      = "/v1/health"
	PathStartJob    = "/v1/actions/{action}/jobs"
	PathJob         = "/v1/jobs/{job_id}"
	PathCancelJob   = "/v1/jobs/{job_id}/cancel"
	PathJobEvents   = "/v1/jobs/{job_id}/events"
	PathJobMessages = "/v1/jobs/{job_id}/messages"
	CorePathTrigger = "/api/v1/triggers"
)

const (
	HeaderAuthorization = "Authorization"
	HeaderPluginName    = "X-Plugin-Name"
	BearerPrefix        = "Bearer "
)

const DefaultMaxBodyBytes int64 = 16 << 20

type ErrorCode string

const (
	CodeInvalidInput     ErrorCode = "invalid_input"
	CodeUnknownAction    ErrorCode = "unknown_action"
	CodeJobNotFound      ErrorCode = "job_not_found"
	CodeJobConflict      ErrorCode = "job_conflict"
	CodeJobNotActive     ErrorCode = "job_not_active"
	CodeUnsupported      ErrorCode = "unsupported"
	CodeUnauthorized     ErrorCode = "unauthorized"
	CodePayloadTooLarge  ErrorCode = "payload_too_large"
	CodeInternal         ErrorCode = "internal"
	CodeUpstreamFailure  ErrorCode = "upstream_failure"
	CodeProtocolMismatch ErrorCode = "protocol_mismatch"
)

type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}
