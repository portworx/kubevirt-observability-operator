package verify

import (
	"fmt"
	"net"
	"time"
)

// TCP checks if the given host and port are reachable.
func TCP(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
