package main

import (
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	negroni_gzip "github.com/phyber/negroni-gzip/gzip"
	"github.com/xyproto/permissions2"
)

var RootHandler = func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, "root")
}

var DropHandler = func(w http.ResponseWriter, req *http.Request) {
	if ungzippedBody, err := gzip.NewReader(req.Body); err == nil {
		if contents, err := ioutil.ReadAll(ungzippedBody); err == nil {
			fmt.Fprintln(w, string(contents))
		}
	} else {
		http.Error(w, "Could not read file contents.", http.StatusInternalServerError)
	}
}

var DeniedFunction = func(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "Permission denied!", http.StatusForbidden)
}

func main() {
	// gorilla mux
	r := mux.NewRouter().StrictSlash(false)
	r.HandleFunc("/", RootHandler)
	r.HandleFunc("/drop", DropHandler).
		Methods("POST").
		HeadersRegexp("Content-Type", "application/gzip")

	// negroni
	n := negroni.Classic()

	// middleware instantiations

	// requires redis, default port is 6379
	perm := permissions.New()

	// Custom handler for when permissions are denied
	perm.SetDenyFunction(DeniedFunction)

	// middleware
	n.Use(perm)
	n.Use(negroni_gzip.Gzip(negroni_gzip.DefaultCompression))

	// handlers
	n.UseHandler(r)

	n.Run(":8080")
}
