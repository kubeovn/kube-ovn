package ovn_leader_checker

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

const staleMemberTimeoutEnv = "OVN_LEADER_STALE_MEMBER_TIMEOUT"

var (
	raftStatusLine  = regexp.MustCompile(`^Status:\s+(.+)$`)
	raftRoleLine    = regexp.MustCompile(`^Role:\s+(\S+)`)
	raftLeaderLine  = regexp.MustCompile(`^Leader:\s+(\S+)`)
	raftServerLine  = regexp.MustCompile(`^\s+([[:xdigit:]]+)\s+\(([[:xdigit:]]+)\s+at\s+(.+?)\)(.*)$`)
	lastMessageLine = regexp.MustCompile(`last msg ([0-9]+) ms ago`)
	matchIndexLine  = regexp.MustCompile(`match_index=([0-9]+)`)
	raftPhaseLine   = regexp.MustCompile(`\((?:adding:\s*)?(?:catchup|caught up|committing|remove|removing)\)`)
)

type raftMember struct {
	sid           string
	address       string
	self          bool
	lastMessage   time.Duration
	hasLastMsg    bool
	matchIndex    uint64
	hasMatchIndex bool
}

type raftClusterStatus struct {
	status             string
	role               string
	leader             string
	members            []raftMember
	hasReconfiguration bool
}

func staleMemberTimeoutFromEnv() time.Duration {
	value := os.Getenv(staleMemberTimeoutEnv)
	if value == "" {
		return defaultStaleMemberTimeout
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		klog.Warningf("ignoring invalid %s=%q, using %s", staleMemberTimeoutEnv, value, defaultStaleMemberTimeout)
		return defaultStaleMemberTimeout
	}
	return time.Duration(seconds) * time.Second
}

func parseRaftClusterStatus(output string) (raftClusterStatus, error) {
	var status raftClusterStatus
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case raftStatusLine.MatchString(line):
			status.status = strings.TrimSpace(raftStatusLine.FindStringSubmatch(line)[1])
		case raftRoleLine.MatchString(line):
			status.role = raftRoleLine.FindStringSubmatch(line)[1]
		case raftLeaderLine.MatchString(line):
			status.leader = raftLeaderLine.FindStringSubmatch(line)[1]
		case strings.Contains(line, "Adding server ") || strings.Contains(line, "Removing server "):
			status.hasReconfiguration = true
		}

		match := raftServerLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if raftPhaseLine.MatchString(match[4]) {
			status.hasReconfiguration = true
		}
		member := raftMember{
			sid:     match[1],
			address: canonicalRaftAddress(match[3]),
			self:    strings.Contains(match[4], "(self)"),
		}
		if last := lastMessageLine.FindStringSubmatch(match[4]); last != nil {
			milliseconds, err := strconv.ParseInt(last[1], 10, 64)
			if err != nil {
				return raftClusterStatus{}, fmt.Errorf("parse last message for %s: %w", member.sid, err)
			}
			member.lastMessage = time.Duration(milliseconds) * time.Millisecond
			member.hasLastMsg = true
		}
		if index := matchIndexLine.FindStringSubmatch(match[4]); index != nil {
			matchIndex, err := strconv.ParseUint(index[1], 10, 64)
			if err != nil {
				return raftClusterStatus{}, fmt.Errorf("parse match index for %s: %w", member.sid, err)
			}
			member.matchIndex = matchIndex
			member.hasMatchIndex = true
		}
		status.members = append(status.members, member)
	}

	if status.status == "" || status.role == "" || status.leader == "" || len(status.members) == 0 {
		return raftClusterStatus{}, fmt.Errorf("incomplete cluster status: status=%q role=%q leader=%q members=%d", status.status, status.role, status.leader, len(status.members))
	}
	return status, nil
}

func canonicalRaftAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func (s raftClusterStatus) hasQuorum(timeout time.Duration) bool {
	quorum := len(s.members)/2 + 1
	fresh := 0
	for _, member := range s.members {
		if member.self || (member.hasLastMsg && member.lastMessage < timeout) {
			fresh++
		}
	}
	return fresh >= quorum
}

func (s raftClusterStatus) staleDuplicate(timeout time.Duration) (raftMember, bool) {
	if timeout <= 0 {
		timeout = defaultStaleMemberTimeout
	}
	if s.status != "cluster member" || s.role != "leader" || s.leader == "unknown" || s.hasReconfiguration || !s.hasQuorum(timeout) {
		return raftMember{}, false
	}

	groups := make(map[string][]raftMember)
	for _, member := range s.members {
		groups[member.address] = append(groups[member.address], member)
	}
	for _, members := range groups {
		if len(members) != 2 {
			continue
		}
		var stale, active raftMember
		for _, member := range members {
			isStale := !member.self && (!member.hasLastMsg || member.lastMessage >= timeout) && member.hasMatchIndex
			if isStale {
				if stale.sid != "" {
					stale = raftMember{}
					break
				}
				stale = member
				continue
			}
			if member.hasMatchIndex && (member.self || (member.hasLastMsg && member.lastMessage < timeout)) {
				if active.sid != "" {
					active = raftMember{}
					break
				}
				active = member
			}
		}
		// A departed follower retains its last replicated index in this term.
		// Require its replacement to have caught up before removing that SID.
		if stale.sid != "" && active.sid != "" && active.matchIndex > 0 && active.matchIndex >= stale.matchIndex {
			return stale, true
		}
	}
	return raftMember{}, false
}

func (c *Configuration) observeDuplicateMember(database, address, sid string, duplicate bool) (int, bool) {
	if c.duplicateMemberObservations == nil {
		c.duplicateMemberObservations = make(map[string]int)
	}
	key := database + ":" + address + ":" + sid
	if !duplicate {
		delete(c.duplicateMemberObservations, key)
		return 0, false
	}
	for observed := range c.duplicateMemberObservations {
		if strings.HasPrefix(observed, database+":") && observed != key {
			delete(c.duplicateMemberObservations, observed)
		}
	}
	c.duplicateMemberObservations[key]++
	count := c.duplicateMemberObservations[key]
	return count, count >= maxDuplicateMemberObservations
}

func cleanStaleDuplicateMember(c *Configuration, database string) {
	output, err := ovnDatabaseControl(database, "cluster/status", databaseName(database))
	if err != nil {
		c.resetDuplicateMemberObservations(database)
		klog.Warningf("failed to inspect %s cluster members: %v", database, err)
		return
	}
	status, err := parseRaftClusterStatus(output)
	if err != nil {
		c.resetDuplicateMemberObservations(database)
		klog.Warningf("failed to parse %s cluster status: %v", database, err)
		return
	}
	candidate, found := status.staleDuplicate(c.staleMemberTimeout)
	if !found {
		c.resetDuplicateMemberObservations(database)
		return
	}
	count, confirmed := c.observeDuplicateMember(database, candidate.address, candidate.sid, true)
	if !confirmed {
		klog.Infof("found stale duplicate %s member %s at %s (%d/%d observations)", database, candidate.sid, candidate.address, count, maxDuplicateMemberObservations)
		return
	}
	if _, err = ovnDatabaseControl(database, "cluster/kick", databaseName(database), candidate.sid); err != nil {
		klog.Warningf("failed to kick stale duplicate %s member %s at %s: %v", database, candidate.sid, candidate.address, err)
		return
	}
	klog.Infof("kicked stale duplicate %s member %s at %s", database, candidate.sid, candidate.address)
	c.observeDuplicateMember(database, candidate.address, candidate.sid, false)
}

func ovnDatabaseControl(database, command string, args ...string) (string, error) {
	controlSocket := fmt.Sprintf("/var/run/ovn/ovn%s_db.ctl", database)
	commandArgs := append([]string{"-t", controlSocket, command}, args...)
	output, err := exec.Command("ovn-appctl", commandArgs...).CombinedOutput() // #nosec G204
	if err != nil {
		return "", fmt.Errorf("failed to run ovn-appctl for %s: %w", database, err)
	}
	return string(output), nil
}

func (c *Configuration) resetDuplicateMemberObservations(database string) {
	for key := range c.duplicateMemberObservations {
		if strings.HasPrefix(key, database+":") {
			delete(c.duplicateMemberObservations, key)
		}
	}
}

func databaseName(database string) string {
	switch database {
	case "nb", ovnnb.DatabaseName:
		return ovnnb.DatabaseName
	case "sb", ovnsb.DatabaseName:
		return ovnsb.DatabaseName
	default:
		return database
	}
}
