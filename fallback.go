package main

import (
	"errors"
	"io/ioutil"
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
	resp, err := client.Get(url)

	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return &FileBrokenError{"Trouble making GET request!"}
	}

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.New("Trouble reading reesponse body!")
	}

	err = ioutil.WriteFile(out, contents, 0644)
	if err != nil {
		return errors.New("Trouble creating file!")
	}
	return nil
}
