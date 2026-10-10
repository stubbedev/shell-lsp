package jsonrpc

import (
	"bytes"
	"testing"
)

func FuzzRead(f *testing.F) {
	seeds := []string{
		"Content-Length: 2\r\n\r\n{}",
		"Content-Length: 100\r\n\r\nshort",
		"Content-Length: -1\r\n\r\n",
		"Content-Length: 99999999999999999999\r\n\r\n",
		"Garbage\r\n\r\nbody",
		"\r\n\r\n",
		"Content-Length: 13\r\n\r\n{\"id\":1,\"m\":",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		conn := NewConn(bytes.NewReader([]byte(data)), nil)
		for range 16 {
			if _, err := conn.Read(); err != nil {
				return
			}
		}
	})
}
