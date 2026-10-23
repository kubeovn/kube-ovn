package kohelper

import "fmt"

// ConsumeArgumentValue returns the next command-line value and the remaining
// arguments. Keeping this small parser primitive in the shared helper package
// keeps the client and node-agent argument handling consistent.
func ConsumeArgumentValue(flag string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s requires a value", flag)
	}
	return args[0], args[1:], nil
}
