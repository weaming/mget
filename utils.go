package main

import (
	"log"
	"os"
	"path"
	"strings"
)

func deleteFile(path string) {
	err := os.Remove(path)
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("deleted file: %v\n", path)
}

func PrepareDir(filePath string, force bool) {
	filePath = os.ExpandEnv(filePath)
	if !force && !strings.HasSuffix(filePath, "/") {
		filePath = path.Dir(filePath)
	}
	err := os.MkdirAll(filePath, os.FileMode(0755))
	if err != nil {
		log.Fatal(err)
	}
}
