# sriovnet
Go library to configure SRIOV networking devices

Local build and test

You can use go get command:
```
go get github.com/Mellanox/sriovnet
```

Example:

```go
package main

import (
    "fmt"

    "github.com/Mellanox/sriovnet"
)

func main() {
	var vfList[10] *sriovnet.VfObj

	err1 := sriovnet.EnableSriov("ib0")
	if err1 != nil {
		return
	}

	handle, err2 := sriovnet.GetPfNetdevHandle("ib0")
	if err2 != nil {
		return
	}
	err3 := sriovnet.ConfigVfs(handle, false)
	if err3 != nil {
		return
	}
	for i := 0; i < 10; i++ {
		vfList[i], _ = sriovnet.AllocateVf(handle)
	}
	for _, vf := range handle.List {
		fmt.Printf("after allocation vf = %v\n", vf)
	}
	for i := 0; i < 10; i++ {
		if vfList[i] == nil {
			continue
		}
		sriovnet.FreeVf(handle, vfList[i])
	}
	for _, vf := range handle.List {
		fmt.Printf("after free vf = %v\n", vf)
	}
}
```
