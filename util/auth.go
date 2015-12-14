package util

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
)

func BasicAuth(r *http.Request) (user, pass string) {
	// ripped out of httpauth library
	const basicScheme string = "Basic "

	// Confirm the request is sending Basic Authentication credentials.
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, basicScheme) {
		return
	}

	// Get the plain-text username and password from the request
	// The first six characters are skipped - e.g. "Basic ".
	str, err := base64.StdEncoding.DecodeString(auth[len(basicScheme):])
	if err != nil {
		return
	}

	// Split on the first ":" character only, with any subsequent colons assumed to be part
	// of the password. Note that the RFC2617 standard does not place any limitations on
	// allowable characters in the password.
	creds := bytes.SplitN(str, []byte(":"), 2)

	if len(creds) != 2 {
		return
	}

	return string(creds[0]), string(creds[1])
}
