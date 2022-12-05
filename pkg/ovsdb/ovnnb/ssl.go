// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnnb

const SSLTable = "SSL"

// SSL defines an object in SSL table
type SSL struct {
	UUID            string            `ovsdb:"_uuid"`
	BootstrapCaCert bool              `ovsdb:"bootstrap_ca_cert"`
	CaCert          string            `ovsdb:"ca_cert"`
	Certificate     string            `ovsdb:"certificate"`
	ExternalIDs     map[string]string `ovsdb:"external_ids"`
	PrivateKey      string            `ovsdb:"private_key"`
	SSLCiphers      string            `ovsdb:"ssl_ciphers"`
	SSLProtocols    string            `ovsdb:"ssl_protocols"`
}
