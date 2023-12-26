package main

import (
	"log"

	"github.com/beam-cloud/beam/internal/worker"
)

func main() {
	s, err := worker.NewWorker()
	if err != nil {
		log.Fatal(err)
	}

	err = s.Run()
	if err != nil {
		log.Fatalf("Worker exited with error: %v\n", err)
	}
}