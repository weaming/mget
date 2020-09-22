package main

import (
	"log"
	"os"
	"os/signal"
	"path"
	"strings"
)

func deleteFile(path string) {
	err := os.Remove(path)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("deleted file:", path)
}

func PrepareDir(filePath string) {
	filePath = os.ExpandEnv(filePath)
	if !strings.HasSuffix(filePath, "/") {
		filePath = path.Dir(filePath)
	}
	err := os.MkdirAll(filePath, os.FileMode(0755))
	if err != nil {
		log.Fatal(err)
	}
}

func captureInterrupt() {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt
	log.Fatal("Interrupt")
}
