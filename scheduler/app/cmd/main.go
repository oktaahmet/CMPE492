package main

import "x402-scheduler/internal/server"

// @title           x402 Scheduler API
// @version         1.0
// @description     Worker registration, job pull, result submit, payments, stats.
// @host            localhost:8080
// @BasePath        /
func main() {
	server.Main()
}
