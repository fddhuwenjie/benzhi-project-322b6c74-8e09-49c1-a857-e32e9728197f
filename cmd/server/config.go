package main

import (
	"flag"
	"os"
	"strconv"
)

func address() string {
	def := "127.0.0.1:19091"
	if p := os.Getenv("PORT"); p != "" {
		if n, e := strconv.Atoi(p); e == nil && n > 0 && n < 65536 {
			def = "127.0.0.1:" + p
		}
	}
	a := flag.String("addr", def, "listen address")
	flag.Parse()
	return *a
}
