package main

import (
	"flag"
	"fmt"
	"icecoreacclimationgate/internal/application"
	"icecoreacclimationgate/internal/audit"
	"icecoreacclimationgate/internal/persistence"
	transport "icecoreacclimationgate/internal/transport/http"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	self := flag.Bool("self-check", false, "run self check")
	addr := address()
	dir := filepath.Join(os.TempDir(), "icecore-acclimation")
	if *self {
		dir = filepath.Join(os.TempDir(), "icecore-selfcheck-"+fmt.Sprint(time.Now().UnixNano()))
	}
	store, e := persistence.Open(dir)
	if e != nil {
		panic(e)
	}
	chain, e := audit.Open(dir)
	if e != nil {
		panic(e)
	}
	srv := transport.New(application.New(store, chain))
	if *self {
		go func() { _ = http.ListenAndServe(addr, srv.Handler()) }()
		if e = waitHealth(addr); e != nil {
			panic(e)
		}
		if e = transport.SelfCheck("http://" + addr); e != nil {
			panic(e)
		}
		fmt.Println("自检通过")
		return
	}
	fmt.Println("监听 " + addr)
	if e = http.ListenAndServe(addr, srv.Handler()); e != nil {
		panic(e)
	}
}
