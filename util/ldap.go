package util

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"

	"github.com/milescrabill/mozldap"
)

type LdapConfig struct {
	Uri, Uid, Ou, Dc, Username, Password, ClientCertFile, ClientKeyFile, CaCertFile string
	Insecure, Starttls                                                              bool
}

func ConfigureLdapClient(conf LdapConfig) (*mozldap.Client, error) {
	// import the client certificates
	cert, err := tls.LoadX509KeyPair(conf.ClientCertFile, conf.ClientKeyFile)
	if err != nil {
		return nil, err
	}

	// import the ca cert
	ca := x509.NewCertPool()
	CAcert, err := ioutil.ReadFile(conf.CaCertFile)
	if err != nil {
		return nil, err
	}

	if ok := ca.AppendCertsFromPEM(CAcert); !ok {
		log.Fatal("failed to import CA Certificate")
	}

	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            ca,
		InsecureSkipVerify: true,
	}

	// instantiate an ldap client
	ldapClient, err := mozldap.NewClient(
		conf.Uri,
		conf.Username,
		conf.Password,
		&tlsConfig,
		conf.Starttls)
	if err != nil {
		return nil, err
	}

	return &ldapClient, nil
}
