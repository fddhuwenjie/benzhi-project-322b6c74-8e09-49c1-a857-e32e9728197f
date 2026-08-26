package main

import (
	"fmt"
	"net/http"
	"time"
)

func waitHealth(addr string) error {
	for i := 0; i < 30; i++ {
		r, e := http.Get("http://" + addr + "/healthz")
		if e == nil {
			r.Body.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("health check timeout")
}
