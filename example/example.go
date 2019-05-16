package main

import (
	"log"
	"net/http"

	"github.com/sg3des/loom"
)

func main() {
	log.SetFlags(log.Lshortfile)
	loom.Debug = true

	l := loom.NewLoom()
	l.SetHandler("hello", hello)

	http.Handle("/", http.FileServer(http.Dir(".")))
	http.Handle("/ws", l.Handler())
	http.ListenAndServe("127.0.0.1:8080", nil)
}

type helloData struct {
	Name string
}

func hello(req *helloData) (*helloData, error) {
	log.Println(req)

	return &helloData{
		Name: "Hello, " + req.Name,
	}, nil
}
