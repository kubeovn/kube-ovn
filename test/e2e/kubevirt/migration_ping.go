package kubevirt

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// migrationPing runs independently of the exec session until explicitly stopped.
// Its deadline is only a safety net; reaching it must not produce a passing test.
type migrationPing struct {
	dir  string
	exec func(string) (string, string, error)
}

func (p *migrationPing) run(command string) (string, error) {
	stdout, stderr, err := p.exec("cd " + quotePingArgument(p.dir) + " && " + command)
	if err != nil {
		return stdout, fmt.Errorf("migration ping: %w; stdout: %s; stderr: %s", err, stdout, stderr)
	}
	return stdout, nil
}

func quotePingArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (p *migrationPing) start(ip string, deadlineSeconds int) error {
	// The supervisor reaps ping and writes its status only after stdout is flushed.
	command := fmt.Sprintf(`
mkdir -p %s && cd %s || exit 1
nohup sh -c '
  LC_ALL=C ping -n -i 0.1 -w "$1" "$2" > output 2>&1 &
  pid=$!
  echo "$pid" > pid
  wait "$pid"
  echo "$?" > status
  mv status done
' sh %d %s </dev/null >/dev/null 2>&1 &
`, quotePingArgument(p.dir), quotePingArgument(p.dir), deadlineSeconds, quotePingArgument(ip))
	stdout, stderr, err := p.exec(command)
	if err != nil {
		return fmt.Errorf("start migration ping: %w; stdout: %s; stderr: %s", err, stdout, stderr)
	}
	return nil
}

func (p *migrationPing) ready() error {
	// A running process alone does not prove packets are being sent and received.
	_, err := p.run(`test ! -f done && grep -q 'bytes from' output`)
	return err
}

func (p *migrationPing) stop() (string, error) {
	return p.run(`
if [ -f done ]; then
  cat output
  echo 'ping exited before migration observation completed' >&2
  exit 1
fi
kill -INT "$(cat pid)" || exit 1
for i in $(seq 1 50); do
  if [ -f done ]; then
    cat output
    status=$(cat done)
    test "$status" -le 1
    exit $?
  fi
  sleep 0.1
done
echo 'timed out waiting for ping statistics' >&2
exit 1
`)
}

func (p *migrationPing) cleanup() error {
	// Also runs when migration creation or completion fails. Do not signal a
	// reaped PID, which could already have been reused by another process.
	_, _, err := p.exec(`if [ -d ` + quotePingArgument(p.dir) + ` ]; then
  cd ` + quotePingArgument(p.dir) + ` || exit 1
  for i in $(seq 1 50); do
    if [ -f done ]; then
      rm -f output pid status done
      cd / && rmdir ` + quotePingArgument(p.dir) + `
      exit $?
    fi
    if [ -f pid ]; then
      kill -INT "$(cat pid)" 2>/dev/null || true
    fi
    sleep 0.1
  done
  exit 1
fi`)
	return err
}

func parsePingStats(stdout string) (transmitted, received, lost int, err error) {
	re := regexp.MustCompile(`(?m)(\d+) packets transmitted, (\d+)(?: packets)? received`)
	matches := re.FindStringSubmatch(stdout)
	if len(matches) != 3 {
		return 0, 0, 0, fmt.Errorf("failed to parse ping statistics from output %q", stdout)
	}
	transmitted, err = strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, 0, err
	}
	received, err = strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, 0, err
	}
	return transmitted, received, transmitted - received, nil
}
