package agent

import (
	"errors"
	"fmt"
	"testing"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
)

func TestClassifyErrorNil(t *testing.T) {
	if got := ClassifyError(nil); got != nil {
		t.Errorf("ClassifyError(nil) = %v, want nil", got)
	}
}

func TestClassifyErrorNotLoggedInIsFatal(t *testing.T) {
	err := fmt.Errorf("getting token: %w", auth.ErrNotLoggedIn)
	got := ClassifyError(err)
	if !errors.Is(got, ErrFatal) {
		t.Errorf("ClassifyError(%v) is not ErrFatal, want it to be", err)
	}
	if !errors.Is(got, auth.ErrNotLoggedIn) {
		t.Error("ClassifyError() lost the original auth.ErrNotLoggedIn in the chain")
	}
}

func TestClassifyErrorStatusCodes(t *testing.T) {
	cases := []struct {
		status    int
		wantFatal bool
	}{
		{401, true},
		{403, true},
		{404, true},
		{400, false},
		{409, false},
		{500, false},
		{503, false},
	}
	for _, c := range cases {
		apiErr := &api.Error{StatusCode: c.status, Code: "x", Message: "boom"}
		wrapped := fmt.Errorf("registering chunk: %w", apiErr)

		got := ClassifyError(wrapped)
		gotFatal := errors.Is(got, ErrFatal)
		if gotFatal != c.wantFatal {
			t.Errorf("status %d: ClassifyError() fatal = %v, want %v", c.status, gotFatal, c.wantFatal)
		}
		if !errors.Is(got, apiErr) {
			t.Errorf("status %d: ClassifyError() lost the original *api.Error in the chain", c.status)
		}
	}
}

func TestClassifyErrorGenericNetworkFailureIsRetryable(t *testing.T) {
	err := fmt.Errorf("calling register-chunk: %w", errors.New("dial tcp: connection refused"))
	got := ClassifyError(err)
	if errors.Is(got, ErrFatal) {
		t.Error("ClassifyError() marked a generic network failure as fatal, want retryable")
	}
}
