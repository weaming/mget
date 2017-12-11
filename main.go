package main

// interrupt by ^C -> download function -> stop downloading -> release file -> check and delete file
// download success -> close file -> check and delete file

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	fp "path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HTTP GET timeout
const TIMEOUT = 20

var outfile string
var multiParts = true

var cancelByUser = make(chan bool, 1)

// must use buffer
var closedFile = make(chan bool, 1)

// handle the signal SIGINT
var interruptSignalChannel = make(chan os.Signal, 1)

var client = &http.Client{
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 30,
	},
	Timeout: TIMEOUT * time.Second,
}

type FileBrokenError struct {
	msg string // description of error
}

func (e *FileBrokenError) Error() string {
	return e.msg
}

func main() {
	flag.StringVar(&outfile, "o", outfile, "Output file path.")
	flag.BoolVar(&multiParts, "m", multiParts, "Download the file by multiple parts")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s URL\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	// call Parse() first!
	flag.Parse()

	url := flag.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Please give the URL!\n")
		os.Exit(1)
	}

	if flag.Arg(1) != "" {
		outfile = flag.Arg(1)
	}

	if outfile == "" {
		outfile = fp.Base(url)
	}

	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		log.Printf("Time took %s", elapsed)
	}()

	// create dir if not exists
	outdir := fp.Dir(outfile)
	var _, err = os.Stat(outdir)
	if os.IsNotExist(err) {
		err := os.MkdirAll(outdir, 0755)
		if err != nil {
			panic(err)
		}
	}

	//signal.Notify(interruptSignalChannel, syscall.SIGINT, syscall.SIGQUIT)
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
		default:
			log.Println(err)
		case *FileBrokenError:
			log.Println("broken file error:", err)
		}
		os.Exit(3)
	} else {
		log.Printf("%v => %v\n", url, outfile)
		// download success
	}
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
			os.Remove(out)
		}
	}()

	FileDownloader, err := NewFileDownloader(url, outfile, -1)
	if err != nil {
		return err
	}

	// finish downloading or canceled by user, print result
	var exitSignal = make(chan bool)
	// if canceled by user then pause downloading
	go func() {
		// block until exitSignal by user
		<-cancelByUser
		// notify downloader to exitSignal
		FileDownloader.Pause()
		exitSignal <- true
		err = errors.New("canceled by user!")
		// sleep 3 second gives chance to call os.Exit(2)
		time.Sleep(time.Second * time.Duration(3))
	}()

	FileDownloader.OnFinish(func() {
		exitSignal <- true
	})

	FileDownloader.OnError(func(errCode int, err error) {
		log.Println(errCode, err)
	})

	var waitForDownloadFinish sync.WaitGroup
	FileDownloader.OnStart(func() {
		log.Printf("start download %v\n", out)
		log.Printf("total size: %v\n", FileDownloader.HumanSize())
		format := "\r%12d/%v [%s] %9d kB/s %v"
		var lastSpeed int64
		for {
			status := FileDownloader.GetStatus()
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
