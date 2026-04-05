package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ExecutionError struct {
	ExitCode  int
	Retriable bool
	Code      string
	Message   string
	Stderr    string
	Cause     error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"plugin execution failed"}
	if e.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit=%d", e.ExitCode))
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.Stderr) != "" {
		parts = append(parts, "stderr="+e.Stderr)
	}
	return strings.Join(parts, " ")
}

func (e *ExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ExecuteRequest(ctx context.Context, binaryPath string, request RequestEnvelope, timeout time.Duration) (ResponseEnvelope, error) {
	binaryPath = strings.TrimSpace(binaryPath)
	if binaryPath == "" {
		return ResponseEnvelope{}, fmt.Errorf("binary path cannot be empty")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if strings.TrimSpace(request.Version) == "" {
		request.Version = EnvelopeVersionV1
	}

	rawRequest, err := json.Marshal(request)
	if err != nil {
		return ResponseEnvelope{}, fmt.Errorf("encode plugin request: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, binaryPath)
	cmd.Stdin = bytes.NewReader(rawRequest)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := extractExitCode(runErr)
	stdoutText := strings.TrimSpace(stdout.String())
	stderrText := strings.TrimSpace(stderr.String())

	response, decodeErr := decodeResponseEnvelope(stdoutText)

	if runErr != nil {
		execErr := &ExecutionError{
			ExitCode:  exitCode,
			Retriable: retriableByExitCode(exitCode),
			Stderr:    stderrText,
			Cause:     runErr,
		}
		if response.Error != nil {
			execErr.Code = strings.TrimSpace(response.Error.Code)
			execErr.Message = strings.TrimSpace(response.Error.Message)
			if response.Retriable {
				execErr.Retriable = true
			}
		}
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			execErr.Code = "DEADLINE_EXCEEDED"
			execErr.Message = "plugin execution timeout exceeded"
			execErr.Retriable = true
		}
		if decodeErr != nil && stdoutText != "" {
			execErr.Message = mergeMessage(execErr.Message, fmt.Sprintf("decode response failed: %v", decodeErr))
		}
		return ResponseEnvelope{}, execErr
	}

	if decodeErr != nil {
		return ResponseEnvelope{}, fmt.Errorf("decode plugin response: %w", decodeErr)
	}
	if strings.TrimSpace(response.Version) == "" {
		response.Version = EnvelopeVersionV1
	}
	if !response.OK {
		execErr := &ExecutionError{
			ExitCode:  exitCode,
			Retriable: response.Retriable || retriableByExitCode(exitCode),
			Cause:     fmt.Errorf("plugin returned ok=false"),
		}
		if response.Error != nil {
			execErr.Code = strings.TrimSpace(response.Error.Code)
			execErr.Message = strings.TrimSpace(response.Error.Message)
		}
		return ResponseEnvelope{}, execErr
	}

	return response, nil
}

func decodeResponseEnvelope(raw string) (ResponseEnvelope, error) {
	if strings.TrimSpace(raw) == "" {
		return ResponseEnvelope{}, fmt.Errorf("empty response payload")
	}
	var response ResponseEnvelope
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return ResponseEnvelope{}, err
	}
	return response, nil
}

func extractExitCode(runErr error) int {
	if runErr == nil {
		return ExitCodeSuccess
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func retriableByExitCode(code int) bool {
	switch code {
	case ExitCodeTransient, ExitCodeTimeout, ExitCodeInternal:
		return true
	default:
		return false
	}
}

func mergeMessage(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	switch {
	case current == "":
		return next
	case next == "":
		return current
	default:
		return current + "; " + next
	}
}

func newRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}
