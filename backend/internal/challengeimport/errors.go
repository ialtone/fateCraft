package challengeimport

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/challenge"
)

type PackageError struct {
	Code  string
	Cause error
}

func (e *PackageError) Error() string { return e.Cause.Error() }
func (e *PackageError) Unwrap() error { return e.Cause }
func packageError(code, format string, args ...any) error {
	return &PackageError{Code: code, Cause: fmt.Errorf("%w: %s", challenge.ErrInvalidChallenge, fmt.Sprintf(format, args...))}
}
