package main

// interrupt by ^C -> download function -> stop downloading -> release file -> check and delete file
// download success -> close file -> check and delete file

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	fp "path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	outfile    string
	multiParts = true

	cancelByUser = make(chan bool, 1)
	// must use buffer
	closedFile = make(chan bool, 1)
	// handle the signal SIGINT
	interruptSignalChannel = make(chan os.Signal, 1)
)

func main() {
	flag.StringVar(&outfile, "o", outfile, "Output file path.")
	flag.BoolVar(&multiParts, "m", multiParts, "Download the file by multiple parts")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s URL\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	// call Parse() first!
	flag.Parse()

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

	Start(url, outfile)
}

func Start(url, outfile string) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		log.Printf("Time took %s", elapsed)
	}()

	PrepareDir(outfile, false)
	signal.Notify(interruptSignalChannel, syscall.SIGINT)

	// add, done if download success or exit explicitly
	go func() {
		for range interruptSignalChannel {
			// captured the cancel signal
			cancelByUser <- true
			// wait for releasing the file
			<-closedFile
			deleteFile(outfile)
			os.Exit(2)
		}
	}()

	downloadIt(url, outfile)
}

func downloadIt(url, outfile string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in downloadIt(): ", r)
		}
	}()

	var _, err = os.Stat(outfile)
	if err == nil {
		fmt.Printf("Ignore existed: %v\n", outfile)
		os.Exit(1)
		return
	}

	if multiParts {
		err = multiRangeDownload(url, outfile)
		if err != nil {
			log.Println(err)
			err = downloadAsOne(url, outfile)
		}
	} else {
		err = downloadAsOne(url, outfile)
	}

	if err != nil {
		switch err.(type) {
		case *FileBrokenError:
			log.Println("broken file:", err)
		default:
			log.Println(err)
		}
		os.Exit(3)
	} else {
		// download success
		log.Printf("%v => %v\n", url, outfile)
	}
}

func multiRangeDownload(url, out string) (err error) {
	outfile, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		// get size
		fi, _ := outfile.Stat()
		size := fi.Size()
		// then close it
		outfile.Close()
		// notify the file has been released
		closedFile <- true

		// delete it if blank
		if size == 0 {
			deleteFile(out)
		}
	}()

	FileDownloader, err := NewFileDownloader(url, outfile, -1)
	if err != nil {
		return err
	}

	// finish downloading or canceled by user, print result
	var exitSignal = make(chan bool)
	go func() {
		// block until exitSignal by user
		<-cancelByUser
		exitSignal <- true
		err = errors.New("canceled by user!")
		// sleep 3 second gives chance to call os.Exit(2)
		time.Sleep(time.Second * time.Duration(3))
	}()

	FileDownloader.OnFinish(func() {
		exitSignal <- true
	})

	FileDownloader.OnError(func(err error) {
		log.Println(err)
	})

	var waitForDownloadFinish sync.WaitGroup
	FileDownloader.OnStart(func() {
		log.Printf("start download %v\n", out)
		log.Printf("total size: %v\n", FileDownloader.HumanSize())
		format := "\r%12d/%v [%s] %9d KB/s %v"

		var lastSpeed int64
		for {
			// put here to sync between FileDownloader and current goroutine
			status := FileDownloader.Status()
			i := float64(status.Downloaded) / float64(FileDownloader.Size) * 50
			h := strings.Repeat("=", int(i)) + strings.Repeat(" ", 50-int(i))

			select {
			case <-exitSignal:
				// finish downloading
				fmt.Printf(format, status.Downloaded, FileDownloader.Size, h, lastSpeed, "[ FINISHED! ]\n")
				os.Stdout.Sync()
				waitForDownloadFinish.Done()
				return
			default:
				lastSpeed = status.Speeds / 1024
				fmt.Printf(format, status.Downloaded, FileDownloader.Size, h, lastSpeed, "[DOWNLOADING]")
				os.Stdout.Sync()
				time.Sleep(time.Second * 1)
			}
		}
	})

	waitForDownloadFinish.Add(1)
	FileDownloader.Start()
	waitForDownloadFinish.Wait()
	return
}
