package main

// interrupt by ^C -> download function -> stop downloading -> release file -> check and delete file
// download success -> close file -> check and delete file

import (
	"flag"
	"fmt"
	"log"
	"os"
	fp "path/filepath"
	"time"
)

var (
	outfile    string
	multiParts = true
)

func init() {
	flag.StringVar(&outfile, "o", outfile, "Output file path.")
	flag.BoolVar(&multiParts, "m", multiParts, "Download the file by multiple parts")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s URL\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
}

func main() {
	url := flag.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Please give the URL!\n")
		os.Exit(1)
	}

	if outfile == "" {
		outfile = fp.Base(url)
	}

	start(url, outfile)
}

func start(url, outfile string) {
	defer func(start time.Time) {
		elapsed := time.Since(start)
		log.Printf("Time took %s", elapsed)
	}(time.Now())
	PrepareDir(outfile)

	done := make(chan bool)
	go captureInterrupt(done)

	go func() {
		downloadIt(url, outfile)
		done <- true
	}()

	<-done
}

func downloadIt(url, outfile string) error {
	if _, err := os.Stat(outfile); err == nil {
		log.Println("file already exists:", outfile)
		return nil
	}

	var err error
	if multiParts {
		if err = multiRangeDownload(url, outfile); err != nil {
			log.Println(err)
			err = downloadAsOne(url, outfile)
		}
	} else {
		err = downloadAsOne(url, outfile)
	}

	if err != nil {
		return err
	}

	log.Printf("%v => %v\n", url, outfile)
	return nil
}

func multiRangeDownload(url, out string) (err error) {
	log.Println("download using ranges...")

	outfile, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		fi, _ := outfile.Stat()
		size := fi.Size()
		outfile.Close()

		if size == 0 {
			deleteFile(out)
		}
	}()

	dl, err := NewFileDownloader(url, outfile, -1)
	if err != nil {
		return err
	}

	// finish downloading or canceled by user, print result
	finishChan := make(chan bool)
	dl.OnFinish(func() {
		finishChan <- true
	})

	dl.OnError(func(err error) {
		log.Println(err)
	})

	done := make(chan bool)
	dl.OnStart(func() {
		log.Printf("start download %v\n", out)
		log.Printf("total size: %v\n", dl.HumanSize())
		format := "\r %9d KB/s %v"

		status := dl.Status
		var lastSpeed int64
		ticker := time.NewTicker(time.Millisecond * 500)
		defer ticker.Stop()

		for {
			select {
			case <-finishChan:
				status.WithLock(func() {
					fmt.Printf(format, lastSpeed, "[ FINISHED! ]")
					os.Stdout.Sync()
				}, false)
				done <- true
				return
			case <-ticker.C:
				status.WithLock(func() {
					lastSpeed = status.Speeds / 1024
					fmt.Printf(format, lastSpeed, "[DOWNLOADING]")
					os.Stdout.Sync()
				}, false)
			}
		}
	})

	go dl.Start()
	<-done
	return
}
