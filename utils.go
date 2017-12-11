package main

import (
	"fmt"
	"log"
	"os"
)

func isError(err error) bool {
	if err != nil {
		fmt.Println(err.Error())
	}

	return err != nil
}

func deleteFile(path string) {
	var err = os.Remove(path)
	if isError(err) {
		return
	}

	log.Printf("deleted unfinished file: %v\n", path)
}
