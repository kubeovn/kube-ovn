// Copyright 2018 Paul Greenberg (greenpau@outlook.com)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovsdb

import (
	"strings"
)

// Error - TODO
type Error struct {
	Message string `json:"error"`
	Details string `json:"details"`
	Syntax  string `json:"syntax"`
}

func (e *Error) String() string {
	var s strings.Builder
	s.WriteString(e.Message)
	if e.Details != "" {
		s.WriteString(": ")
		s.WriteString(e.Details)
	}
	if e.Syntax != "" {
		s.WriteString(", syntax: ")
		s.WriteString(e.Syntax)
	}
	return s.String()
}
