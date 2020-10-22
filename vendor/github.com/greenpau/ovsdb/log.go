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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

func readLogFile(f OvsDataFile) (map[string]map[string]uint64, int64, error) {
	var offset int64
	var size int64
	var stats = map[string]map[string]uint64{}
	file, err := os.Open(f.Path)
	if err != nil {
		return stats, offset, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return stats, offset, err
	}
	size = info.Size()
	if f.Reader.Offset == 0 {
		// The reader tracks only incremental changes.
		// When this functions is being invoked for the first time,
		// it just keeps the record of an offset.
		return stats, size, nil
	}
	if f.Reader.Offset > size {
		// The reader detected a new file.
		// Thus, it will read from its beginning.
		offset = 0
	} else {
		offset = f.Reader.Offset
	}
	bufSize := size - offset
	if bufSize < 1 {
		// Nothing to read
		return stats, offset, nil
	}
	buf := make([]byte, bufSize)
	if _, err := file.ReadAt(buf, offset); err != nil {
		return stats, offset, nil
	}
	br := bytes.NewReader(buf)
	reader := bufio.NewReader(br)
	var nr int64
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if len(line) > 0 {
			nr = nr + int64(len(line))
			elements := strings.Split(strings.TrimSuffix(line, "\n"), "|")
			if len(elements) < 5 {
				continue
			}
			//timestamp := elements[0]
			//seq := elements[1]
			source := elements[2]
			severity := strings.ToLower(elements[3])
			//message := elements[4]
			if _, exists := stats[severity]; !exists {
				stats[severity] = make(map[string]uint64)
			}
			if _, exists := stats[severity][source]; !exists {
				stats[severity][source] = 1
				continue
			}
			stats[severity][source]++
		}
	}
	if nr > 0 {
		offset = offset + nr
	}
	return stats, offset, nil
}

// GetLogFileEventStats TODO
func (cli *OvnClient) GetLogFileEventStats(name string) (map[string]map[string]uint64, error) {
	switch name {
	case "ovsdb-server":
		stats, offset, err := readLogFile(cli.Database.Vswitch.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Database.Vswitch.File.Log.Reader.Offset = offset
		return stats, nil
	case "ovsdb-server-northbound":
		stats, offset, err := readLogFile(cli.Database.Northbound.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Database.Northbound.File.Log.Reader.Offset = offset
		return stats, nil
	case "ovsdb-server-southbound":
		stats, offset, err := readLogFile(cli.Database.Southbound.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Database.Southbound.File.Log.Reader.Offset = offset
		return stats, nil
	case "ovn-northd":
		stats, offset, err := readLogFile(cli.Service.Northd.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Service.Northd.File.Log.Reader.Offset = offset
		return stats, nil
	case "ovs-vswitchd":
		stats, offset, err := readLogFile(cli.Service.Vswitchd.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Service.Vswitchd.File.Log.Reader.Offset = offset
		return stats, nil
	}
	return nil, fmt.Errorf("The '%s' component is unsupported", name)
}

// GetLogFileEventStats TODO
func (cli *OvsClient) GetLogFileEventStats(name string) (map[string]map[string]uint64, error) {
	switch name {
	case "ovsdb-server":
		stats, offset, err := readLogFile(cli.Database.Vswitch.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Database.Vswitch.File.Log.Reader.Offset = offset
		return stats, nil
	case "ovs-vswitchd":
		stats, offset, err := readLogFile(cli.Service.Vswitchd.File.Log)
		if err != nil {
			return stats, err
		}
		cli.Service.Vswitchd.File.Log.Reader.Offset = offset
		return stats, nil
	}
	return nil, fmt.Errorf("The '%s' component is unsupported", name)
}

// GetLogFileInfo TODO
func (cli *OvnClient) GetLogFileInfo(name string) (OvsDataFile, error) {
	var i os.FileInfo
	var err error
	switch name {
	case "ovsdb-server":
		i, err = os.Stat(cli.Database.Vswitch.File.Log.Path)
		if err == nil {
			cli.Database.Vswitch.File.Log.Info = i
			cli.Database.Vswitch.File.Log.Component = name
			return cli.Database.Vswitch.File.Log, nil
		}
	case "ovsdb-server-northbound":
		i, err = os.Stat(cli.Database.Northbound.File.Log.Path)
		if err == nil {
			cli.Database.Northbound.File.Log.Info = i
			cli.Database.Northbound.File.Log.Component = name
			return cli.Database.Northbound.File.Log, nil
		}
	case "ovsdb-server-southbound":
		i, err = os.Stat(cli.Database.Southbound.File.Log.Path)
		if err == nil {
			cli.Database.Southbound.File.Log.Info = i
			cli.Database.Southbound.File.Log.Component = name
			return cli.Database.Southbound.File.Log, nil
		}
	case "ovn-northd":
		i, err = os.Stat(cli.Service.Northd.File.Log.Path)
		if err == nil {
			cli.Service.Northd.File.Log.Info = i
			cli.Service.Northd.File.Log.Component = name
			return cli.Service.Northd.File.Log, nil
		}
	case "ovs-vswitchd":
		i, err = os.Stat(cli.Service.Vswitchd.File.Log.Path)
		if err == nil {
			cli.Service.Vswitchd.File.Log.Info = i
			cli.Service.Vswitchd.File.Log.Component = name
			return cli.Service.Vswitchd.File.Log, nil
		}
	default:
		return OvsDataFile{}, fmt.Errorf("The '%s' component is unsupported", name)
	}
	return OvsDataFile{}, err
}

// GetLogFileInfo TODO
func (cli *OvsClient) GetLogFileInfo(name string) (OvsDataFile, error) {
	var i os.FileInfo
	var err error
	switch name {
	case "ovsdb-server":
		i, err = os.Stat(cli.Database.Vswitch.File.Log.Path)
		if err == nil {
			cli.Database.Vswitch.File.Log.Info = i
			cli.Database.Vswitch.File.Log.Component = name
			return cli.Database.Vswitch.File.Log, nil
		}
	case "ovs-vswitchd":
		i, err = os.Stat(cli.Service.Vswitchd.File.Log.Path)
		if err == nil {
			cli.Service.Vswitchd.File.Log.Info = i
			cli.Service.Vswitchd.File.Log.Component = name
			return cli.Service.Vswitchd.File.Log, nil
		}
	default:
		return OvsDataFile{}, fmt.Errorf("The '%s' component is unsupported", name)
	}
	return OvsDataFile{}, err
}
