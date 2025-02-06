package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
)

var LogTag = setLogTag()

const defaultLogTag = "restorebrigadier"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

func fatal(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	switch jout {
	case true:
		answ := ministry.Answer{
			Answer: dcmgmt.Answer{
				Answer: keydesk.Answer{
					Code:    http.StatusInternalServerError,
					Desc:    http.StatusText(http.StatusInternalServerError),
					Status:  keydesk.AnswerStatusError,
					Message: msg,
				},
			},
		}

		payload, err := json.Marshal(answ)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't marshal answer: %s\n", LogTag, err)
		}

		if _, err := w.Write(payload); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't write answer: %s\n", LogTag, err)
		}
	default:
		fmt.Fprint(w, msg)
	}

	log.Fatal(msg)
}

func tooEarly(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	switch jout {
	case true:
		answ := ministry.Answer{
			Answer: dcmgmt.Answer{
				Answer: keydesk.Answer{
					Code:    http.StatusTooEarly,
					Desc:    http.StatusText(http.StatusTooEarly),
					Status:  keydesk.AnswerStatusError,
					Message: msg,
				},
			},
		}

		payload, err := json.Marshal(answ)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't marshal answer: %s\n", LogTag, err)
		}

		if _, err := w.Write(payload); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't write answer: %s\n", LogTag, err)
		}
	default:
		fmt.Fprint(w, msg)
	}

	fmt.Fprintln(os.Stderr, msg)
}
