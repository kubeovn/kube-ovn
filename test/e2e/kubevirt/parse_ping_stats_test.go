package kubevirt

import "testing"

func TestParsePingStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stdout  string
		tx      int
		rx      int
		lost    int
		wantErr bool
	}{
		{
			name:   "iputils",
			stdout: "3 packets transmitted, 3 received, 0% packet loss, time 2003ms",
			tx:     3,
			rx:     3,
			lost:   0,
		},
		{
			name:   "busybox",
			stdout: "3 packets transmitted, 3 packets received, 0% packet loss",
			tx:     3,
			rx:     3,
			lost:   0,
		},
		{
			name:   "iputils with loss",
			stdout: "400 packets transmitted, 395 received, 1% packet loss, time 60000ms",
			tx:     400,
			rx:     395,
			lost:   5,
		},
		{
			name:    "unparseable",
			stdout:  "ping: bad address",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx, rx, lost, err := parsePingStats(tt.stdout)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tx != tt.tx || rx != tt.rx || lost != tt.lost {
				t.Fatalf("got tx=%d rx=%d lost=%d, want tx=%d rx=%d lost=%d", tx, rx, lost, tt.tx, tt.rx, tt.lost)
			}
		})
	}
}
