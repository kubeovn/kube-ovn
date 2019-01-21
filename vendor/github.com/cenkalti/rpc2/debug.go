package rpc2

import "log"

// DebugLog controls the printing of internal and I/O errors.
var DebugLog = false

func debugln(v ...interface{}) {
	if DebugLog {
		log.Println(v...)
	}
}
