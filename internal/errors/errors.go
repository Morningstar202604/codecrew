// Package errors 提供统一的错误类型和错误码，便于精细错误处理。
package errors

import (
	"errors"
	"fmt"
)

// 错误码定义
const (
	// 配置相关
	CodeConfigNotFound     = "CONFIG_NOT_FOUND"
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeConfigMissingKey   = "CONFIG_MISSING_KEY"
	CodeProviderNotFound   = "PROVIDER_NOT_FOUND"
	CodeProviderMissingURL = "PROVIDER_MISSING_URL"

	// 模型相关
	CodeModelNotSpecified  = "MODEL_NOT_SPECIFIED"
	CodeModelInvalidFormat = "MODEL_INVALID_FORMAT"
	CodeModelNotFound      = "MODEL_NOT_FOUND"
	CodeLLMRequestFailed   = "LLM_REQUEST_FAILED"
	CodeLLMStreamFailed    = "LLM_STREAM_FAILED"
	CodeLLMRateLimit       = "LLM_RATE_LIMIT"
	CodeLLMTimeout         = "LLM_TIMEOUT"

	// 工具相关
	CodeToolNotFound        = "TOOL_NOT_FOUND"
	CodeToolDenied          = "TOOL_DENIED"
	CodeToolExecutionFailed = "TOOL_EXECUTION_FAILED"
	CodeToolTimeout         = "TOOL_TIMEOUT"

	// 文件相关
	CodeFileNotFound    = "FILE_NOT_FOUND"
	CodeFileReadFailed  = "FILE_READ_FAILED"
	CodeFileWriteFailed = "FILE_WRITE_FAILED"
	CodeFilePermission  = "FILE_PERMISSION_DENIED"

	// 会话相关
	CodeSessionNotFound   = "SESSION_NOT_FOUND"
	CodeSessionLoadFailed = "SESSION_LOAD_FAILED"

	// 通用
	CodeInvalidArgument = "INVALID_ARGUMENT"
	CodeInternalError   = "INTERNAL_ERROR"
	CodeNotImplemented  = "NOT_IMPLEMENTED"
	CodeTimeout         = "TIMEOUT"
)

// AppError 是应用级错误，包含错误码、消息和底层错误。
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

// Error 实现 error 接口。
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is 和 errors.As。
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New 创建一个新的 AppError。
func New(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap 包装底层错误为 AppError。
func Wrap(code, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Cause: cause}
}

// Is 检查错误是否匹配指定错误码。
func Is(err error, code string) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

// As 尝试将错误转换为 AppError。
func As(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// 常用错误快捷构造函数

// ConfigNotFound 创建配置未找到错误。
func ConfigNotFound(path string) *AppError {
	return New(CodeConfigNotFound, fmt.Sprintf("配置文件不存在: %s", path))
}

// ConfigInvalid 创建配置无效错误。
func ConfigInvalid(path string, err error) *AppError {
	return Wrap(CodeConfigInvalid, fmt.Sprintf("配置文件 %s 解析失败", path), err)
}

// ProviderNotFound 创建供应商未找到错误。
func ProviderNotFound(name string, available []string) *AppError {
	return New(CodeProviderNotFound, fmt.Sprintf("未知供应商 %q，已配置: %v", name, available))
}

// ModelNotSpecified 创建模型未指定错误。
func ModelNotSpecified() *AppError {
	return New(CodeModelNotSpecified, "未指定模型")
}

// ModelInvalidFormat 创建模型格式无效错误。
func ModelInvalidFormat(spec string) *AppError {
	return New(CodeModelInvalidFormat, fmt.Sprintf("模型格式应为 供应商/模型名，例如 deepseek/deepseek-chat；当前: %s", spec))
}

// LLMRequestFailed 创建 LLM 请求失败错误。
func LLMRequestFailed(status string, body string) *AppError {
	return New(CodeLLMRequestFailed, fmt.Sprintf("模型返回 %s：%s", status, body))
}

// LLMStreamFailed 创建 LLM 流式读取失败错误。
func LLMStreamFailed(err error) *AppError {
	return Wrap(CodeLLMStreamFailed, "读取模型流失败", err)
}

// ToolNotFound 创建工具未找到错误。
func ToolNotFound(name string) *AppError {
	return New(CodeToolNotFound, fmt.Sprintf("未知工具: %s", name))
}

// ToolDenied 创建工具被拒绝错误。
func ToolDenied(name string) *AppError {
	return New(CodeToolDenied, fmt.Sprintf("工具 %s 已被禁用", name))
}

// ToolExecutionFailed 创建工具执行失败错误。
func ToolExecutionFailed(name string, err error) *AppError {
	return Wrap(CodeToolExecutionFailed, fmt.Sprintf("工具 %s 执行失败", name), err)
}

// FileNotFound 创建文件未找到错误。
func FileNotFound(path string) *AppError {
	return New(CodeFileNotFound, fmt.Sprintf("文件不存在: %s", path))
}

// FileReadFailed 创建文件读取失败错误。
func FileReadFailed(path string, err error) *AppError {
	return Wrap(CodeFileReadFailed, fmt.Sprintf("读取文件失败: %s", path), err)
}

// FileWriteFailed 创建文件写入失败错误。
func FileWriteFailed(path string, err error) *AppError {
	return Wrap(CodeFileWriteFailed, fmt.Sprintf("写入文件失败: %s", path), err)
}

// InvalidArgument 创建无效参数错误。
func InvalidArgument(arg, message string) *AppError {
	return New(CodeInvalidArgument, fmt.Sprintf("参数 %s 无效: %s", arg, message))
}

// Internal 创建内部错误。
func Internal(message string, err error) *AppError {
	return Wrap(CodeInternalError, message, err)
}

// NotImplemented 创建未实现错误。
func NotImplemented(feature string) *AppError {
	return New(CodeNotImplemented, fmt.Sprintf("功能未实现: %s", feature))
}
