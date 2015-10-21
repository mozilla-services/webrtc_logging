package main

import (
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/satori/go.uuid"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	negroni_gzip "github.com/phyber/negroni-gzip/gzip"
	"github.com/xyproto/permissions2"
)

var s3Client *s3.S3
var uploader *s3manager.Uploader

const logs_bucket = "webrtc_logs"

var RootHandler = func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, "root")
}

var DropHandler = func(w http.ResponseWriter, req *http.Request) {
	if ungzippedBody, err := gzip.NewReader(req.Body); err != nil {
		http.Error(w, "Could not read logs.", http.StatusInternalServerError)
	} else {
		if _, err := ioutil.ReadAll(ungzippedBody); err != nil {
			http.Error(w, "Could not decode logs.", http.StatusInternalServerError)
		} else {
			// generate uuid
			uuid := uuid.NewV4()

			result, err := uploader.Upload(&s3manager.UploadInput{
				Bucket:          aws.String(logs_bucket),
				Key:             aws.String(fmt.Sprintf("%s-%s", uuid, time.Now().Format(time.RFC3339))),
				Body:            req.Body,
				ContentEncoding: aws.String("gzip"),
			})
			if err != nil {
				http.Error(w, fmt.Sprintf("Could not upload logs. Error: %s", err), http.StatusInternalServerError)
			} else {
				fmt.Fprintln(w, fmt.Sprintf("Upload success, uuid: %, result: %s", uuid, result))
			}
		}
	}
}

var DeniedFunction = func(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "Permission denied!", http.StatusForbidden)
}

func init() {
	// default aws region
	defaults.DefaultConfig.Region = aws.String("us-west-2")

	// s3
	s3Client = s3.New(nil)

	// make sure bucket exists
	result, err := s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(logs_bucket),
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.GoString())
}

func main() {
	// s3
	uploader = s3manager.NewUploader(nil)

	// gorilla mux
	router := mux.NewRouter().StrictSlash(false)
	router.HandleFunc("/", RootHandler)
	router.HandleFunc("/drop", DropHandler).
		Methods("POST").
		HeadersRegexp("Content-Type", "application/gzip")

	// negroni
	neg := negroni.Classic()

	// middleware instantiations
	// requires redis, default port is 6379
	perm := permissions.New()

	// Custom handler for when permissions are denied
	perm.SetDenyFunction(DeniedFunction)

	// middleware
	neg.Use(perm)
	neg.Use(negroni_gzip.Gzip(negroni_gzip.DefaultCompression))

	// handlers
	neg.UseHandler(router)
	neg.Run(":8080")
}
