package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	negroni_gzip "github.com/phyber/negroni-gzip/gzip"

	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"

	"github.com/mozilla-services/mozldap"
	bucketlister "github.com/mozilla-services/product-delivery-tools/bucketlister/services"
)

var s3Client *s3.S3
var ldapClient mozldap.Client
var uploader *s3manager.Uploader
var rootLister *bucketlister.BucketLister

var config = flag.String("c", "config.yaml", "Load configuration from file")

const logsBucketName = "webrtc-logs"

type conf struct {
	Ldap struct {
		Uri, Username, Password string
		Insecure, Starttls      bool
	}
}

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

var testHandler = func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, "test")
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
	rootLister.ServeHTTP(w, req)
}

func init() {
	flag.Parse()

	// load the local configuration file
	fd, err := ioutil.ReadFile(*config)
	if err != nil {
		log.Fatal(err)
	}

	// ldap conf
	var conf conf

	err = yaml.Unmarshal(fd, &conf)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	log.Printf("%v", conf)

	// instantiate an ldap client
	ldapClient, err = mozldap.NewClient(
		conf.Ldap.Uri,
		conf.Ldap.Username,
		conf.Ldap.Password,
		&tls.Config{InsecureSkipVerify: conf.Ldap.Insecure},
		conf.Ldap.Starttls)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("connected %s on %s:%d, tls:%v starttls:%v\n", ldapClient.BaseDN, ldapClient.Host, ldapClient.Port, ldapClient.UseTLS, ldapClient.UseStartTLS)

	// default aws region
	defaults.DefaultConfig.Region = aws.String("us-west-2")

	// s3
	s3Client = s3.New(nil)

	// check for logs_bucket existence
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
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
	// router := bone.New()
	router := mux.NewRouter()
	router.HandleFunc(".*[^\\.]", bucketlisterHandler).Methods("GET")
	router.HandleFunc("^/.*\\.[^/]+$", testHandler).Methods("GET")
	router.HandleFunc("/", uploadHandler).Methods("POST")

	// negroni
	neg := negroni.Classic()

	// middleware
	neg.Use(negroni_gzip.Gzip(negroni_gzip.DefaultCompression))

	// handlers
	neg.UseHandler(router)
	neg.Run(":8080")
}
