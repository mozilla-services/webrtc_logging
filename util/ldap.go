package util

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"
	"regexp"

	"github.com/asaskevich/govalidator"
	"github.com/mozilla-services/mozldap"
)

type LdapConfig struct {
	Uri, Username, Password, ClientCertFile, ClientKeyFile, CaCertFile, Dc string
	Insecure, Starttls                                                     bool
}

func ConfigureLdapClient(conf LdapConfig) (*mozldap.Client, error) {
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
		InsecureSkipVerify: conf.Insecure,
	}

	// check if ldap email was entered
	if govalidator.IsEmail(conf.Username) {
		conf.Username = "mail=" + conf.Username + ",o=com,dc=" + conf.Dc
	}

	// instantiate an ldap client
	ldapClient, err := mozldap.NewTLSClient(
		conf.Uri,
		conf.Username,
		conf.Password,
		conf.ClientCertFile,
		conf.ClientKeyFile,
		conf.CaCertFile,
		&tlsConfig,
		conf.Starttls)

	return &ldapClient, err
}

func GetAllowedUsers(config LdapConfig, groups []string) (map[string]bool, error) {
	allowedUsers := make(map[string]bool)
	lc, err := ConfigureLdapClient(config)
	if err != nil {
		return allowedUsers, err
	}
	users, err := lc.GetUsersInGroups(groups)
	if err != nil {
		return allowedUsers, err
	}
	for _, user := range users {
		// get only the email address
		email := regexp.MustCompile("[^=]+=([^,]+),.*").FindStringSubmatch(user)[1]
		allowedUsers[email] = true
	}

	return allowedUsers, nil
}
