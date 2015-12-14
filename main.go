package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"

	bucketlister "github.com/milescrabill/product-delivery-tools/bucketlister/services"
	util "github.com/milescrabill/webrtc_logging/util"
)

var sess *session.Session
var s3Client *s3.S3
var conf Config

type Config struct {
	S3 struct {
		BucketName, Region string
	}
	Server struct {
		URI string
	}
	Ldap util.LdapConfig
}

func handleMultipartForm(req *http.Request, folderName string) (err error) {
	// 24K allocated for files
	const _24K = (1 << 20) * 24
	if err = req.ParseMultipartForm(_24K); err != nil {
		return
	}

	uploader := s3manager.NewUploader(sess)
	for _, fileHeaders := range req.MultipartForm.File {
		for _, header := range fileHeaders {
			var file multipart.File
			if file, err = header.Open(); err != nil {
				return
			}

			_, err = uploader.Upload(&s3manager.UploadInput{
				Bucket:          aws.String(conf.S3.BucketName),
				Key:             aws.String(fmt.Sprintf("%s/%s", folderName, header.Filename)),
				Body:            file,
				ContentEncoding: aws.String("gzip"),
			})
			if err != nil {
				return
			}
		}
	}
	return
}

var downloadFileHandler = func(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	bucketName := vars["dir"]
	fileName := vars["file"]
	s3req, _ := s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(conf.S3.BucketName),
		Key:    aws.String(bucketName + "/" + fileName),
	})

	presigned, err := s3req.Presign(time.Minute * 10)
	if err != nil {
		http.Error(w, err.Error(), 503)
	}

	http.Redirect(w, req, presigned, 307)
}

func authenticationWrapper(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		uid, pass := util.BasicAuth(req)

		conf.Ldap.Username = "uid=" + uid + ",ou=" + conf.Ldap.Ou + ",dc=" + conf.Ldap.Dc
		conf.Ldap.Password = pass

		_, err := util.ConfigureLdapClient(conf.Ldap)
		if err != nil {
			log.Println(err.Error())
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm=%q`, "WebRTC Logs"))
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		w.Header().Set("X-Authenticated-Username", uid)
		log.Printf("successfully authenticated as %s\n", uid)
		fn(w, req)
	}
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
		url := conf.Server.URI + folderName + "/"
		log.Printf("upload success: %s", url)
		fmt.Fprintln(w, url)
	}
}

func main() {
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("Missing configuration path.")
	}

	// load the local configuration file
	fd, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err.Error())
	}

	// configuration object
	err = yaml.Unmarshal(fd, &conf)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	sess = session.New(&aws.Config{Region: aws.String(conf.S3.Region)})

	// s3
	s3Client = s3.New(sess)

	// check for logs bucket existence
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(conf.S3.BucketName),
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	// bucketlister
	lister := bucketlister.NewBucketLister(
		conf.S3.BucketName,
		"",
		sess.Config,
	)

	// gorilla mux
	router := mux.NewRouter()
	router.HandleFunc("/", uploadHandler).Methods("POST")
	router.HandleFunc("/", authenticationWrapper(lister.ServeHTTP)).Methods("GET")
	router.HandleFunc("/{dir}/", authenticationWrapper(lister.ServeHTTP)).Methods("GET")
	router.HandleFunc("/{dir}/{file}", authenticationWrapper(downloadFileHandler)).Methods("GET")

	// negroni
	neg := negroni.Classic()

	// handlers
	neg.UseHandler(router)
	neg.Run(":8080")
}
