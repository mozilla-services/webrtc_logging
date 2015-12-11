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
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gorilla/mux"

	"crypto/x509"

	"github.com/codegangsta/negroni"
	"github.com/milescrabill/mozldap"
	bucketlister "github.com/milescrabill/product-delivery-tools/bucketlister/services"
	uuid "github.com/satori/go.uuid"
	"gopkg.in/yaml.v2"
)

var sess *session.Session
var s3Client *s3.S3
var ldapClient mozldap.Client
var uploader *s3manager.Uploader
var rootLister *bucketlister.BucketLister
var conf Config

type Config struct {
	Ldap struct {
		Uri, Username, Password, ClientCertFile, ClientKeyFile, CaCertFile string
		Insecure, Starttls                                                 bool
	}
	S3 struct {
		BucketName, Region string
	}
	Server struct {
		URI string
	}
}

func handleMultipartForm(req *http.Request, folderName string) (url string, err error) {
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
	url = conf.Server.URI + folderName + "/"
	log.Printf("upload success: %s", url)
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

var uploadHandler = func(w http.ResponseWriter, req *http.Request) {
	// generate uuid and time for folder name
	id := uuid.NewV4()
	timestamp := time.Now().Format(time.RFC3339)

	folderName := timestamp + "-" + id.String()

	url, err := handleMultipartForm(req, folderName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not upload logs. Error: %s", err), http.StatusInternalServerError)
	} else {
		fmt.Fprintln(w, url)
	}
}

func init() {
	flag.Parse()

	if flag.NArg() != 1 {
		log.Panic("Missing configuration path.")
	}

	// load the local configuration file
	fd, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	// configuration object
	err = yaml.Unmarshal(fd, &conf)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// import the client certificates
	cert, err := tls.LoadX509KeyPair(conf.Ldap.ClientCertFile, conf.Ldap.ClientKeyFile)
	if err != nil {
		panic(err)
	}

	// import the ca cert
	ca := x509.NewCertPool()
	CAcert, err := ioutil.ReadFile(conf.Ldap.CaCertFile)
	if err != nil {
		panic(err)
	}

	if ok := ca.AppendCertsFromPEM(CAcert); !ok {
		panic("failed to import CA Certificate")
	}

	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            ca,
		InsecureSkipVerify: true,
	}

	// instantiate an ldap client
	ldapClient, err = mozldap.NewClient(
		conf.Ldap.Uri,
		conf.Ldap.Username,
		conf.Ldap.Password,
		&tlsConfig,
		conf.Ldap.Starttls)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("connected %s on %s:%d, tls:%v starttls:%v\n", ldapClient.BaseDN, ldapClient.Host, ldapClient.Port, ldapClient.UseTLS, ldapClient.UseStartTLS)
	sess = session.New(&aws.Config{Region: aws.String(conf.S3.Region)})

	// s3
	s3Client = s3.New(sess)

	// check for logs_bucket existence
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(conf.S3.BucketName),
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
		if awsError, ok := err.(awserr.Error); ok {
			panic(awsError)
		}
	}

	// bucketlister
	rootLister = bucketlister.NewBucketLister(
		conf.S3.BucketName,
		"",
		sess.Config,
	)
}

func main() {
	// s3
	uploader = s3manager.NewUploader(sess)

	// gorilla mux
	router := mux.NewRouter()
	router.HandleFunc("/", rootLister.ServeHTTP).Methods("GET")
	router.HandleFunc("/{dir}/", rootLister.ServeHTTP).Methods("GET")
	router.HandleFunc("/{dir}/{file}", downloadFileHandler).Methods("GET")
	router.HandleFunc("/", uploadHandler).Methods("POST")

	// negroni
	neg := negroni.Classic()

	// handlers
	neg.UseHandler(router)
	neg.Run(":8080")
}
