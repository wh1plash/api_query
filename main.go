package main

import (
	"api_query/api"
	"api_query/internal"
	"fmt"
)

func main() {
	cfg := internal.MustLoad()
	fmt.Println(cfg)
	server := api.NewAPIServer(
		cfg.HTTPServer.Addr,
		cfg.HTTPServer.ReqTimeout,
		cfg.HTTPServer.IdleTimeout)
	server.Start()

}
