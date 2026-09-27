package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

func TestCodeForError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
	}{
		{"shortcut not found", database.ErrShortcutNotFound, CodeShortcutNotFound, http.StatusNotFound},
		{"card not found", database.ErrCardNotFound, CodeCardNotFound, http.StatusNotFound},
		{"database locked", database.ErrDatabaseLocked, CodeDatabaseLocked, http.StatusServiceUnavailable},
		{"wrapped sentinel", fmt.Errorf("get shortcut: %w", database.ErrShortcutNotFound), CodeShortcutNotFound, http.StatusNotFound},
		{"generic database error", errors.New("syntax error"), CodeDatabaseError, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, status := CodeForError(tc.err)
			if code != tc.wantCode || status != tc.wantStatus {
				t.Fatalf("CodeForError(%v) = (%q, %d), want (%q, %d)", tc.err, code, status, tc.wantCode, tc.wantStatus)
			}
		})
	}

	// Catálogo completo de §7 deve permanecer estável.
	codes := map[string]string{
		CodeValidationFailed:       "VALIDATION_FAILED",
		CodeInvalidJSONBody:        "INVALID_JSON_BODY",
		CodeShortcutNotFound:       "SHORTCUT_NOT_FOUND",
		CodeCardNotFound:           "CARD_NOT_FOUND",
		CodeDatabaseLocked:         "DATABASE_LOCKED",
		CodeDatabaseError:          "DATABASE_ERROR",
		CodeHardwareCollectorFault: "HARDWARE_COLLECTOR_FAULT",
		CodeInternalServerError:    "INTERNAL_SERVER_ERROR",
	}
	for code, want := range codes {
		if code != want {
			t.Fatalf("code constant mismatch: %q != %q", code, want)
		}
	}
}
