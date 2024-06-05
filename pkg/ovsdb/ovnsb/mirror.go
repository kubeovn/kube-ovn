// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnsb

const MirrorTable = "Mirror"

type (
	MirrorFilter = string
	MirrorType   = string
)

var (
	MirrorFilterFromLport MirrorFilter = "from-lport"
	MirrorFilterToLport   MirrorFilter = "to-lport"
	MirrorFilterBoth      MirrorFilter = "both"
	MirrorTypeGre         MirrorType   = "gre"
	MirrorTypeErspan      MirrorType   = "erspan"
	MirrorTypeLocal       MirrorType   = "local"
)

// Mirror defines an object in Mirror table
type Mirror struct {
	UUID        string            `ovsdb:"_uuid"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	Filter      MirrorFilter      `ovsdb:"filter"`
	Index       int               `ovsdb:"index"`
	Name        string            `ovsdb:"name"`
	Sink        string            `ovsdb:"sink"`
	Type        MirrorType        `ovsdb:"type"`
}
