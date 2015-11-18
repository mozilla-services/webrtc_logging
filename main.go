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

	"github.com/codegangsta/negroni"
	"github.com/go-zoo/bone"
	negroni_gzip "github.com/phyber/negroni-gzip/gzip"

	bucketlister "github.com/mozilla-services/product-delivery-tools/bucketlister/services"
	"github.com/satori/go.uuid"
)

var s3Client *s3.S3
var uploader *s3manager.Uploader
var rootLister *bucketlister.BucketLister

const logsBucketName = "webrtc-logs"

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
				Bucket:          aws.String(logsBucketName),
				Key:             aws.String(fmt.Sprintf("%s/%s", folderName, header.Filename)),
				Body:            file,
				ContentEncoding: aws.String("gzip"),
			})
			if err != nil {
				return
			}
			fmt.Println("upload success: ", result.Location)
		}
	}
	return
}

var uploadHandler = func(w http.ResponseWriter, req *http.Request) {
	// generate uuid and time for folder name
	id := uuid.NewV4()
	timestamp := time.Now().Format(time.RFC3339)

	folderName := timestamp + "-" + id.String()

	err := handleMultipartForm(req, folderName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not upload logs. Error: %s", err), http.StatusInternalServerError)
	} else {
		fmt.Fprintln(w, fmt.Sprintf("Upload success. <a href=\"%s\" target=\"_blank\">Copy this url.</a>",
			"http://"))
	}
}

var bucketlisterHandler = func(w http.ResponseWriter, req *http.Request) {
	id := bone.GetValue(req, "id")
	fmt.Printf("id: " + id)
	rootLister.ServeHTTP(w, req)
}

func init() {
	// default aws region
	defaults.DefaultConfig.Region = aws.String("us-west-2")

	// s3
	s3Client = s3.New(nil)

	// check for logs_bucket existence
	_, err := s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(logsBucketName),
	})

	// if there was an error, it is likely that the logs_bucket doesn't exist
	if err != nil {
		if reqErr, ok := err.(awserr.RequestFailure); ok {
			// service error occurred
			if reqErr.StatusCode() == 404 {
				// bucket should be created in cloudformation, so this is obsolete
				// bucket not found -> create the bucket
				// _, err := s3Client.CreateBucket(&s3.CreateBucketInput{
				// 	Bucket: aws.String(logsBucketName),
				// })
				// if err != nil {
				// 	panic(err)
				// }
				return
			}
		}
		panic(err)
	}

	// bucketlister
	rootLister = bucketlister.NewBucketLister(
		logsBucketName,
		"",
		aws.NewConfig(),
	)
}

func main() {
	// s3
	uploader = s3manager.NewUploader(nil)

	// bone mux
	router := bone.New()
	router.GetFunc("/*/$", bucketlisterHandler)
	router.PostFunc("/", uploadHandler)

	// negroni
	neg := negroni.Classic()

	// middleware
	neg.Use(negroni_gzip.Gzip(negroni_gzip.DefaultCompression))

	// handlers
	neg.UseHandler(router)
	neg.Run(":8080")
}
