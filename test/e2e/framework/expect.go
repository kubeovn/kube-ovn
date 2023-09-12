/*
Copyright 2014 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"fmt"
	"regexp"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	macRegex  = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}([0-9A-Fa-f]{2})$`)
	uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-([0-9a-f]{4}-){3}[0-9a-f]{12}$`)
)

// ExpectEqual expects the specified two are the same, otherwise an exception raises
func ExpectEqual(actual, extra interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.Equal(extra), explain...)
}

// ExpectNotEqual expects the specified two are not the same, otherwise an exception raises
func ExpectNotEqual(actual, extra interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.Equal(extra), explain...)
}

// ExpectError expects an error happens, otherwise an exception raises
func ExpectError(err error, explain ...interface{}) {
	gomega.ExpectWithOffset(1, err).To(gomega.HaveOccurred(), explain...)
}

// ExpectNoError checks if "err" is set, and if so, fails assertion while logging the error.
func ExpectNoError(err error, explain ...interface{}) {
	ExpectNoErrorWithOffset(1, err, explain...)
}

// ExpectNoErrorWithOffset checks if "err" is set, and if so, fails assertion while logging the error at "offset" levels above its caller
// (for example, for call chain f -> g -> ExpectNoErrorWithOffset(1, ...) error would be logged for "f").
func ExpectNoErrorWithOffset(offset int, err error, explain ...interface{}) {
	if err == nil {
		return
	}

	// Errors usually contain unexported fields. We have to use
	// a formatter here which can print those.
	prefix := ""
	if len(explain) > 0 {
		if str, ok := explain[0].(string); ok {
			prefix = fmt.Sprintf(str, explain[1:]...) + ": "
		} else {
			prefix = fmt.Sprintf("unexpected explain arguments, need format string: %v", explain)
		}
	}

	// This intentionally doesn't use gomega.Expect. Instead，we take
	// full control over what information is presented where:
	// - The complete error object is logged because it may contain
	//   additional information that isn't included in its error
	//   string.
	// - It is not included in the failure message because
	//   it might make the failure message very large and/or
	//   cause error aggregation to work less well: two
	//   failures at the same code line might not be matched in
	//   https://go.k8s.io/triage because the error details are too
	//   different.
	Logf("Unexpected error: %s\n%s", prefix, format.Object(err, 1))
	Fail(prefix+err.Error(), 1+offset)
}

// ExpectConsistOf expects actual contains precisely the extra elements.
// The ordering of the elements does not matter.
func ExpectConsistOf(actual, extra interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.ConsistOf(extra), explain...)
}

// ExpectContainElement expects actual contains the extra elements.
func ExpectContainElement(actual, extra interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.ContainElement(extra), explain...)
}

// ExpectNotContainElement expects actual does not contain the extra elements.
func ExpectNotContainElement(actual, extra interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.ContainElement(extra), explain...)
}

// ExpectContainSubstring expects actual contains the passed-in substring.
func ExpectContainSubstring(actual, substr string, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.ContainSubstring(substr), explain...)
}

// ExpectNotContainSubstring expects actual does not contain the passed-in substring.
func ExpectNotContainSubstring(actual, substr string, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.ContainSubstring(substr), explain...)
}

// ExpectHaveKey expects the actual map has the key in the keyset
func ExpectHaveKey(actual, key interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.HaveKey(key), explain...)
}

// ExpectHaveKeyWithValue expects the actual map has the passed in key/value pair.
func ExpectHaveKeyWithValue(actual, key, value interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.HaveKeyWithValue(key, value), explain...)
}

// ExpectNotHaveKey expects the actual map does not have the key in the keyset
func ExpectNotHaveKey(actual, key interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.HaveKey(key), explain...)
}

// ExpectNil expects actual is nil
func ExpectNil(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.BeNil(), explain...)
}

// ExpectNotNil expects actual is not nil
func ExpectNotNil(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.BeNil(), explain...)
}

// ExpectEmpty expects actual is empty
func ExpectEmpty(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.BeEmpty(), explain...)
}

// ExpectNotEmpty expects actual is not empty
func ExpectNotEmpty(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.BeEmpty(), explain...)
}

// ExpectHaveLen expects actual has the passed-in length
func ExpectHaveLen(actual interface{}, count int, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.HaveLen(count), explain...)
}

// ExpectTrue expects actual is true
func ExpectTrue(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.BeTrue(), explain...)
}

// ExpectFalse expects actual is false
func ExpectFalse(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.BeTrue(), explain...)
}

// ExpectZero expects actual is the zero value for its type or actual is nil.
func ExpectZero(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).To(gomega.BeZero(), explain...)
}

// ExpectNotZero expects actual is not nil nor the zero value for its type.
func ExpectNotZero(actual interface{}, explain ...interface{}) {
	gomega.ExpectWithOffset(1, actual).NotTo(gomega.BeZero(), explain...)
}

// ExpectUUID expects that the given string is a UUID.
func ExpectUUID(s string) {
	ginkgo.By("verifying the string " + s + " is an UUID")
	ExpectTrue(uuidRegex.MatchString(s))
}

// ExpectMAC expects that the given string is a MAC address.
func ExpectMAC(s string) {
	ginkgo.By("verifying the string " + s + " is a MAC address")
	ExpectTrue(macRegex.MatchString(s))
}

// ExpectIPInCIDR expects that the given IP address in within the CIDR.
func ExpectIPInCIDR(ip, cidr string) {
	ginkgo.By("verifying IP address " + ip + " is within the CIDR " + cidr)
	ExpectTrue(util.CIDRContainIP(cidr, ip))
}
