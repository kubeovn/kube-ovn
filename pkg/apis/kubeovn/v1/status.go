package v1

import (
	"encoding/json"
	"fmt"
)

func (ss *SubnetStatus) Bytes() ([]byte, error) {
	//{"availableIPs":65527,"usingIPs":9} => {"status": {"availableIPs":65527,"usingIPs":9}}
	bytes, err := json.Marshal(ss)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	return []byte(newStr), nil
}
