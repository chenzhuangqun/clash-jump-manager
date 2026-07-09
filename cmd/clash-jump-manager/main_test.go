package main

import "testing"

func TestShouldOpenBrowserCanBeDisabledForVerification(t *testing.T) {
	t.Setenv("CLASH_JUMP_MANAGER_NO_BROWSER", "1")

	if shouldOpenBrowser() {
		t.Fatal("expected browser auto-open to be disabled")
	}
}

func TestShouldOpenBrowserDefaultsToEnabled(t *testing.T) {
	t.Setenv("CLASH_JUMP_MANAGER_NO_BROWSER", "")

	if !shouldOpenBrowser() {
		t.Fatal("expected browser auto-open to be enabled by default")
	}
}
