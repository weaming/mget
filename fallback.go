package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

// HTTP GET timeout
const TIMEOUT = 20

var client = &http.Client{
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 30,
	},
	Timeout: TIMEOUT * time.Second,
}

func downloadAsOne(url, out string) error {
	log.Println("download as one...")

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: making GET request")
	}
	defer resp.Body.Close()

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.New("download: reading response body")
	}

	err = ioutil.WriteFile(out, contents, 0644)
	if err != nil {
		return errors.New("download: creating file")
	}
	return nil
}
