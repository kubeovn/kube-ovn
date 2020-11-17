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
	"encoding/json"
	//"github.com/davecgh/go-spew/spew"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func parseSocket(s string) (string, string, error) {
	if strings.HasPrefix(s, "unix") {
		arr := strings.Split(s, ":")
		return arr[0], arr[1], nil
	}
	return "tcp", s, nil
}

func encodeString(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b[:len(b)]), nil
}

func indentAnalysis(s string) int {
	if s == "" {
		return 0
	}
	for i, c := range s {
		if c != ' ' {
			return i
		}
	}
	return 0
}

func dedupInts(arr []int) []int {
	newArr := []int{}
	sort.Sort(sort.IntSlice(arr))
	var prevItem int
	for i, item := range arr {
		if i == 0 {
			newArr = append(newArr, item)
			prevItem = item
			continue
		}
		if item == prevItem {
			continue
		}
		newArr = append(newArr, item)
		prevItem = item
	}
	return newArr
}

func indentDepthAnalysis(arr []int) (map[int]int, error) {
	depth := make(map[int]int)
	sort.Sort(sort.IntSlice(arr))
	indents := dedupInts(arr)
	for i, indent := range indents {
		depth[indent] = i
	}
	return depth, nil
}

func parseTimeUsed(s string) (float64, error) {
	if s == "never" {
		return 0, nil
	}
	var multiplier float64
	measure := s[len(s)-1:]
	switch measure {
	case "s":
		multiplier = 1
	case "m":
		multiplier = 60
	case "h":
		multiplier = 360
	default:
		return 0, fmt.Errorf("invalid input: %s", s)
	}
	s = strings.TrimRight(s, measure)

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid input: %s", s)
	}
	return (v * multiplier), nil
}
