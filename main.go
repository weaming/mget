package main

// interrupt by ^C -> download function -> stop downloading -> release file -> check and delete file
// download success -> close file -> check and delete file

import (
	"flag"
	"fmt"
	"log"
	"os"
	fp "path/filepath"
	"strings"
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
	// parse url
	url := flag.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Please give the URL!\n")
		os.Exit(1)
	}

	// parse outfile
	if flag.Arg(1) != "" {
		outfile = flag.Arg(1)
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

	go captureInterrupt()
	downloadIt(url, outfile)
}

func downloadIt(url, outfile string) error {
	if _, err := os.Stat(outfile); err == nil {
		log.Println("existed", outfile)
		os.Exit(0)
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
	var exitFnOnStart = make(chan bool)
	dl.OnFinish(func() {
		exitFnOnStart <- true
	})

	dl.OnError(func(err error) {
		log.Println(err)
	})

	done := make(chan bool)
	dl.OnStart(func() {
		log.Printf("start download %v\n", out)
		log.Printf("total size: %v\n", dl.HumanSize())
		format := "\r%12d/%v [%s] %9d KB/s %v"

		var lastSpeed int64
		status := dl.Status
		var progress string
		for {
			// put here to sync between FileDownloader and current goroutine
			status.WithLock(func() {
				i := float64(status.Downloaded) / float64(dl.Size) * 50
				progress = strings.Repeat("=", int(i)) + strings.Repeat(" ", 50-int(i))
			}, false)

			select {
			case <-exitFnOnStart:
				// finish downloading
				status.WithLock(func() {
					fmt.Printf(format, status.Downloaded, dl.Size, progress, lastSpeed, "[ FINISHED! ]")
					os.Stdout.Sync()
				}, false)
				done <- true
				return
			default:
				status.WithLock(func() {
					lastSpeed = status.Speeds / 1024
					fmt.Printf(format, status.Downloaded, dl.Size, progress, lastSpeed, "[DOWNLOADING]")
					os.Stdout.Sync()
				}, false)
			}

			time.Sleep(time.Millisecond * 500)
		}
	})

	go dl.Start()
	<-done
	return
}
