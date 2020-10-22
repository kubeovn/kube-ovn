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
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"strconv"
	"strings"
)

// ClusterPeer contains information about a cluster peer.
type ClusterPeer struct {
	ID         string
	Address    string
	NextIndex  uint64
	MatchIndex uint64
	Connection struct {
		Inbound  int
		Outbound int
	}
}

// ClusterState contains information about the state of a cluster of
// a server perspective.
type ClusterState struct {
	ID           string
	UUID         string
	Database     string
	ClusterID    string
	ClusterUUID  string
	Address      string
	Status       int
	Role         int
	Term         uint64
	IsLeaderSelf int
	IsVotedSelf  int
	Log          struct {
		Low  uint64
		High uint64
	}
	NextIndex           uint64
	MatchIndex          uint64
	NotCommittedEntries uint64
	NotAppliedEntries   uint64
	Peers               map[string]*ClusterPeer
	Connections         struct {
		Inbound  int
		Outbound int
	}
}

// GetAppClusteringInfo returns the counters associated with clustering setup.
func (cli *OvnClient) GetAppClusteringInfo(db string) (ClusterState, error) {
	var app Client
	var dbName string
	var err error
	server := ClusterState{}
	server.Peers = make(map[string]*ClusterPeer)
	cmd := "cluster/status"
	switch db {
	case "ovsdb-server-northbound":
		app, err = NewClient(cli.Database.Northbound.Socket.Control, cli.Timeout)
		dbName = cli.Database.Northbound.Name
	case "ovsdb-server-southbound":
		app, err = NewClient(cli.Database.Southbound.Socket.Control, cli.Timeout)
		dbName = cli.Database.Southbound.Name
	default:
		return server, fmt.Errorf("The '%s' database is unsupported for '%s'", db, cmd)
	}
	if err != nil {
		app.Close()
		return server, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, dbName)
	if err != nil {
		app.Close()
		return server, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return server, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(response, "\\n")
	parserOn := false
	for _, line := range lines {
		if line == "" || line == "\"" {
			continue
		}
		if strings.HasPrefix(line, "Name:") {
			parserOn = true
			s := strings.TrimLeft(line, "Name: ")
			s = strings.Join(strings.Fields(s), " ")
			server.Database = s
			continue
		}
		if !parserOn {
			continue
		}
		if strings.HasPrefix(line, "Cluster ID:") {
			s := strings.TrimLeft(line, "Cluster ID:")
			s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
			arr := strings.Split(s, " ")
			if len(arr) != 2 {
				continue
			}
			server.ClusterID = arr[0]
			server.ClusterUUID = strings.TrimRight(strings.TrimLeft(arr[1], "("), ")")
			if len(server.ClusterUUID) > 4 {
				server.ClusterID = server.ClusterUUID[0:4]
			}
			continue
		} else if strings.HasPrefix(line, "Server ID:") {
			s := strings.TrimLeft(line, "Server ID:")
			s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
			arr := strings.Split(s, " ")
			if len(arr) != 2 {
				continue
			}
			server.ID = arr[0]
			server.UUID = strings.TrimRight(strings.TrimLeft(arr[1], "("), ")")
			if len(server.UUID) > 4 {
				server.ID = server.UUID[0:4]
			}
			continue
		} else if strings.HasPrefix(line, "Address:") {
			s := strings.TrimLeft(line, "Address:")
			s = strings.Join(strings.Fields(s), " ")
			server.Address = s
			continue
		} else if strings.HasPrefix(line, "Status:") {
			s := strings.TrimLeft(line, "Status:")
			s = strings.Join(strings.Fields(s), " ")
			switch s {
			case "cluster member":
				server.Status = 1
			default:
				server.Status = 0
			}
			continue
		} else if strings.HasPrefix(line, "Role:") {
			s := strings.TrimLeft(line, "Role:")
			s = strings.Join(strings.Fields(s), " ")
			switch s {
			case "leader":
				server.Role = 3
			case "candidate":
				server.Role = 2
			case "follower":
				server.Role = 1
			default:
				server.Role = 0
			}
			continue
		} else if strings.HasPrefix(line, "Term:") {
			s := strings.TrimLeft(line, "Term:")
			s = strings.Join(strings.Fields(s), " ")
			if term, err := strconv.ParseUint(s, 10, 64); err == nil {
				server.Term = term
			}
		} else if strings.HasPrefix(line, "Leader:") {
			s := strings.TrimLeft(line, "Leader:")
			s = strings.Join(strings.Fields(s), " ")
			if s == "self" {
				server.IsLeaderSelf = 1
			} else {
				server.IsLeaderSelf = 0
			}
		} else if strings.HasPrefix(line, "Vote:") {
			s := strings.TrimLeft(line, "Vote:")
			s = strings.Join(strings.Fields(s), " ")
			if s == "self" {
				server.IsVotedSelf = 1
			} else {
				server.IsVotedSelf = 0
			}
		} else if strings.HasPrefix(line, "Log:") {
			s := strings.TrimLeft(line, "Log:")
			s = strings.Join(strings.Fields(s), " ")
			s = strings.TrimLeft(s, "[")
			s = strings.TrimRight(s, "]")
			a := strings.Split(s, ",")
			if len(a) != 2 {
				continue
			}
			logLow := a[0]
			logHigh := strings.TrimSpace(a[1])
			if i, err := strconv.ParseUint(logLow, 10, 64); err == nil {
				server.Log.Low = i
			}
			if i, err := strconv.ParseUint(logHigh, 10, 64); err == nil {
				server.Log.High = i
			}
		} else if strings.HasPrefix(line, "Entries not yet committed:") {
			s := strings.TrimLeft(line, "Entries not yet committed:")
			s = strings.Join(strings.Fields(s), " ")
			if i, err := strconv.ParseUint(s, 10, 64); err == nil {
				server.NotCommittedEntries = i
			}
		} else if strings.HasPrefix(line, "Entries not yet applied:") {
			s := strings.TrimLeft(line, "Entries not yet applied:")
			s = strings.Join(strings.Fields(s), " ")
			if i, err := strconv.ParseUint(s, 10, 64); err == nil {
				server.NotAppliedEntries = i
			}
		} else if strings.HasPrefix(line, "Connections:") {
			s := strings.TrimLeft(line, "Connections:")
			s = strings.Join(strings.Fields(s), " ")
			conns := strings.Split(strings.Join(strings.Fields(s), " "), " ")
			for _, entry := range conns {
				if strings.Contains(entry, "(self)") {
					continue
				}
				var peer ClusterPeer
				if strings.HasPrefix(entry, "<-") {
					peerID := strings.TrimLeft(entry, "<-")
					if peerID == "0000" {
						continue
					}
					if _, exists := server.Peers[peerID]; !exists {
						peer = ClusterPeer{}
						peer.ID = peerID
						server.Peers[peerID] = &peer
					}
					peer := server.Peers[peerID]
					peer.Connection.Inbound = 1
				}
				if strings.HasPrefix(entry, "->") {
					peerID := strings.TrimLeft(entry, "->")
					if peerID == "0000" {
						continue
					}
					if _, exists := server.Peers[peerID]; !exists {
						peer = ClusterPeer{}
						peer.ID = peerID
						server.Peers[peerID] = &peer
					}
					peer := server.Peers[peerID]
					peer.Connection.Outbound = 1
				}
			}
		} else if strings.HasPrefix(line, "Servers:") {
			continue
		} else if strings.Contains(line, "next_index=") && strings.Contains(line, "match_index=") {
			s := strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
			arr := strings.Split(s, " ")
			if len(arr) < 5 {
				continue
			}
			isSelf := false
			if strings.Contains(line, "(self)") {
				isSelf = true
			}
			peerID := arr[0]
			var peerAddress string
			var peerNextIndex uint64
			var peerMatchIndex uint64
			for _, e := range arr {
				if strings.HasPrefix(e, "tcp:") || strings.HasPrefix(e, "ssl:") {
					if isSelf != true {
						peerAddress = strings.TrimRight(e, ")")
					}
				} else if strings.HasPrefix(e, "next_index=") {
					nextIndex := strings.TrimLeft(e, "next_index=")
					if i, err := strconv.ParseUint(nextIndex, 10, 64); err == nil {
						if isSelf == true {
							server.NextIndex = i
						} else {
							peerNextIndex = i
						}
					}
				} else if strings.HasPrefix(e, "match_index=") {
					matchIndex := strings.TrimLeft(e, "match_index=")
					if i, err := strconv.ParseUint(matchIndex, 10, 64); err == nil {
						if isSelf == true {
							server.MatchIndex = i
						} else {
							peerMatchIndex = i
						}
					}
				} else {
					// do nothing
				}
			}
			if isSelf != true {
				if _, exists := server.Peers[peerID]; !exists {
					peer := ClusterPeer{}
					peer.ID = peerID
					server.Peers[peerID] = &peer
				}
				peer := server.Peers[peerID]
				peer.NextIndex = peerNextIndex
				peer.MatchIndex = peerMatchIndex
				peer.Address = peerAddress
			}
			continue
		} else {
			// do nothing
		}
	}
	//spew.Dump(server)
	return server, nil
}
