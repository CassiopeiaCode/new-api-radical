package limiter

import (
	"errors"
	"testing"
)

func TestIsNoScriptError(t *testing.T) {
	if !isNoScriptError(errors.New("NOSCRIPT No matching script. Please use EVAL.")) {
		t.Fatal("expected NOSCRIPT errors to be detected")
	}
	if !isNoScriptError(errors.New("rate limit failed: noscript missing")) {
		t.Fatal("expected case-insensitive NOSCRIPT detection")
	}
	if isNoScriptError(errors.New("connection reset by peer")) {
		t.Fatal("did not expect unrelated errors to be detected")
	}
	if isNoScriptError(nil) {
		t.Fatal("did not expect nil to be detected")
	}
}
