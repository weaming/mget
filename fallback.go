package main

import (
	"fmt"
	"io"
	"log"
	"os"
)

func downloadAsOne(url, out string) error {
	log.Println("download as one...")

	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("download: making GET request: %w", err)
	}
	defer resp.Body.Close()

	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("download: reading response body: %w", err)
	}

	err = os.WriteFile(out, contents, 0644)
	if err != nil {
		return fmt.Errorf("download: creating file: %w", err)
	}
	return nil
}
