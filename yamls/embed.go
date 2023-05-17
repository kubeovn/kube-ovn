package yaml

import (
	_ "embed"
)

//go:embed coredns-template.yaml
var CorednsTemplateContent []byte
