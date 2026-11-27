package ovn_leader_checker

import (
	"strings"
	"testing"
	"time"
)

const duplicateMemberStatus = `
Name: OVN_Northbound
Status: cluster member
Role: leader
Leader: self
Servers:
    aaaa (aaaa at ssl:[10.0.0.1]:6643) (self) next_index=42 match_index=41
    bbbb (bbbb at ssl:[10.0.0.2]:6643) last msg 120000 ms ago next_index=0 match_index=0
    cccc (cccc at ssl:[10.0.0.3]:6643) last msg 21 ms ago next_index=42 match_index=41
    dddd (dddd at ssl:[10.0.0.2]:6643) last msg 12 ms ago next_index=42 match_index=41
`

func TestParseRaftClusterStatus(t *testing.T) {
	status, err := parseRaftClusterStatus(duplicateMemberStatus)
	if err != nil {
		t.Fatal(err)
	}
	if status.role != "leader" || status.leader != "self" || len(status.members) != 4 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.members[1].address != "ssl:[10.0.0.2]:6643" || status.members[1].matchIndex != 0 || status.members[1].lastMessage != 120*time.Second {
		t.Fatalf("unexpected stale member: %#v", status.members[1])
	}
}

func TestRaftClusterStatusFindsOnlyStaleDuplicateEndpoint(t *testing.T) {
	status, err := parseRaftClusterStatus(duplicateMemberStatus)
	if err != nil {
		t.Fatal(err)
	}
	candidate, found := status.staleDuplicate(120 * time.Second)
	if !found {
		t.Fatal("expected stale duplicate member")
	}
	if candidate.sid != "bbbb" || candidate.address != "ssl:[10.0.0.2]:6643" {
		t.Fatalf("unexpected candidate: %#v", candidate)
	}
}

func TestRaftClusterStatusDoesNotKickLongShutdownWithoutReplacement(t *testing.T) {
	status, err := parseRaftClusterStatus(`
Status: cluster member
Role: leader
Leader: self
Servers:
    aaaa (aaaa at tcp:10.0.0.1:6643) (self) next_index=42 match_index=41
    bbbb (bbbb at tcp:10.0.0.2:6643) last msg 7200000 ms ago next_index=0 match_index=0
    cccc (cccc at tcp:10.0.0.3:6643) last msg 21 ms ago next_index=42 match_index=41
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := status.staleDuplicate(120 * time.Second); found {
		t.Fatal("a single long-shutdown member must not be treated as a duplicate")
	}
}

func TestRaftClusterStatusRejectsUnsafeDuplicate(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{
			name: "member is still catching up",
			status: `
Status: cluster member
Role: leader
Leader: self
Servers:
    aaaa (aaaa at tcp:10.0.0.1:6643) (self) next_index=42 match_index=41
    bbbb (bbbb at tcp:10.0.0.2:6643) last msg 20 ms ago next_index=0 match_index=0
    cccc (cccc at tcp:10.0.0.2:6643) last msg 21 ms ago next_index=0 match_index=0
`,
		},
		{
			name:   "reconfiguration in progress",
			status: duplicateMemberStatus + "Adding server eeee (eeee at ssl:[10.0.0.4]:6643) (catchup)\n",
		},
		{
			name:   "committing member in server list",
			status: strings.Replace(duplicateMemberStatus, "last msg 12 ms ago", "(adding: committing) last msg 12 ms ago", 1),
		},
		{
			name: "no quorum",
			status: `
Status: cluster member
Role: leader
Leader: self
Servers:
    aaaa (aaaa at tcp:10.0.0.1:6643) (self) next_index=42 match_index=41
    bbbb (bbbb at tcp:10.0.0.2:6643) last msg 120000 ms ago next_index=0 match_index=0
    cccc (cccc at tcp:10.0.0.2:6643) last msg 120000 ms ago next_index=0 match_index=0
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := parseRaftClusterStatus(tt.status)
			if err != nil {
				t.Fatal(err)
			}
			if _, found := status.staleDuplicate(120 * time.Second); found {
				t.Fatal("unsafe duplicate was selected for kick")
			}
		})
	}
}

func TestObserveDuplicateMemberRequiresConsecutiveObservations(t *testing.T) {
	cfg := &Configuration{}
	for i := 1; i <= maxDuplicateMemberObservations; i++ {
		count, confirmed := cfg.observeDuplicateMember("nb", "tcp:10.0.0.2:6643", "bbbb", true)
		if count != i || confirmed != (i == maxDuplicateMemberObservations) {
			t.Fatalf("observation %d: count=%d confirmed=%t", i, count, confirmed)
		}
	}
	count, confirmed := cfg.observeDuplicateMember("nb", "tcp:10.0.0.2:6643", "bbbb", false)
	if count != 0 || confirmed {
		t.Fatalf("observation reset failed: count=%d confirmed=%t", count, confirmed)
	}
}

func TestObserveDuplicateMemberResetsWhenCandidateChanges(t *testing.T) {
	cfg := &Configuration{}
	for _, candidate := range []string{"aaaa", "aaaa", "bbbb", "aaaa"} {
		count, confirmed := cfg.observeDuplicateMember("nb", "tcp:10.0.0.2:6643", candidate, true)
		if confirmed {
			t.Fatalf("candidate %s was confirmed too early at count %d", candidate, count)
		}
	}
	count, confirmed := cfg.observeDuplicateMember("nb", "tcp:10.0.0.2:6643", "aaaa", true)
	if count != 2 || confirmed {
		t.Fatalf("candidate change did not reset continuity: count=%d confirmed=%t", count, confirmed)
	}
}
