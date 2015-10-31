package main

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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

const logs_bucket_name = "webrtc-logs"

func handleMultipartForm(req *http.Request, folderName string) (err error) {
	// 24K allocated for files
	const _24K = (1 << 20) * 24
	if err = req.ParseMultipartForm(_24K); err != nil {
		return
	}

	for _, fileHeaders := range req.MultipartForm.File {
		for _, header := range fileHeaders {
			var file multipart.File
			if file, err = header.Open(); err != nil {
				return
			}

			var result *s3manager.UploadOutput
			result, err = uploader.Upload(&s3manager.UploadInput{
				Bucket:          aws.String(logs_bucket_name),
				Key:             aws.String(fmt.Sprintf("%s/%s", folderName, header.Filename)),
				Body:            file,
				ContentEncoding: aws.String("gzip"),
			})
			if err != nil {
				return
			}
			fmt.Println("result", result)
		}
	}
	return
}

var uploadHandler = func(w http.ResponseWriter, req *http.Request) {
	// generate uuid and time for folder name
	id := uuid.NewV4()
	timestamp := time.Now().Format(time.RFC3339)

	err := handleMultipartForm(req, fmt.Sprintf("%s-%s", timestamp, id))
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not upload logs. Error: %s", err), http.StatusInternalServerError)
	} else {
		fmt.Fprintln(w, fmt.Sprintf("Upload success. <a href=\"%s\" target=\"_blank\">Copy this url.</a>",
			"http://"))
	}
}

var viewHandler = func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, "root")
}

var DeniedFunction = func(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "Permission denied!", http.StatusForbidden)
}

func init() {
	// default aws region
	defaults.DefaultConfig.Region = aws.String("us-west-2")

	// s3
	s3Client = s3.New(nil)

	// check for logs_bucket existence
	_, err := s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(logs_bucket_name),
	})

	// if there was an error, it is likely that the logs_bucket doesn't exist
	if err != nil {
		if reqErr, ok := err.(awserr.RequestFailure); ok {
			// service error occurred
			if reqErr.StatusCode() == 404 {
				// bucket not found -> create the bucket
				_, err := s3Client.CreateBucket(&s3.CreateBucketInput{
					Bucket: aws.String(logs_bucket_name),
				})
				if err != nil {
					panic(err)
				}
				return
			}
		}
		panic(err)
	}
}

func main() {
	// s3
	uploader = s3manager.NewUploader(nil)

	// gorilla mux
	router := mux.NewRouter().StrictSlash(false)
	router.HandleFunc("/", uploadHandler).
		Methods("POST")
	router.HandleFunc("/view/", viewHandler)

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
