package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

var LogTag = setLogTag()

const defaultLogTag = "checkbrigadier"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

// checkAnswer is the JSON payload for -j mode: enough to identify a brigade
// (by name+mnemonics) without generating or returning any credentials.
type checkAnswer struct {
	Code      int    `json:"code"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	BrigadeID string `json:"brigade_id,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
}

func writeAnswer(w io.Writer, jout bool, code int, status, message string, brigadeID uuid.UUID, deleted bool) {
	if !jout {
		if message != "" {
			fmt.Fprintln(w, message)
		}
		return
	}

	answ := checkAnswer{
		Code:    code,
		Status:  status,
		Message: message,
		Deleted: deleted,
	}

	if brigadeID != uuid.Nil {
		answ.BrigadeID = brigadeID.String()
	}

	payload, err := json.Marshal(answ)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: Can't marshal answer: %s\n", LogTag, err)
		return
	}

	if _, err := w.Write(payload); err != nil {
		fmt.Fprintf(os.Stderr, "%s: Can't write answer: %s\n", LogTag, err)
	}
}

// fatal writes an error answer and terminates. Used for unexpected/internal errors.
func fatal(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	writeAnswer(w, jout, http.StatusInternalServerError, "error", msg, uuid.Nil, false)

	log.Fatal(msg)
}

// notFound writes a 404 answer for an unrecognized name/mnemonics pair and returns
// (the caller is expected to return immediately after calling this - no os.Exit).
func notFound(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	writeAnswer(w, jout, http.StatusNotFound, "error", msg, uuid.Nil, false)
}

// success writes a 200 answer carrying the resolved brigade_id.
func success(w io.Writer, jout bool, brigadeID uuid.UUID, deleted bool) {
	writeAnswer(w, jout, http.StatusOK, "success", "", brigadeID, deleted)
}
