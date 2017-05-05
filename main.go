package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	fp "path/filepath"
	"strings"
	"sync"
	"time"
)

// HTTP GET timeout
const TIMEOUT = 20

var wg sync.WaitGroup
var outfile = ""
var multiParts = true

var client = &http.Client{
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 30,
	},
	Timeout: TIMEOUT * time.Second,
}

func main() {
	flag.StringVar(&outfile, "o", outfile, "Output file path.")
	flag.BoolVar(&multiParts, "multi", multiParts, "Download the file by multiple parts")

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

	wg.Add(1)
	go downloadIt(url, outfile)
	wg.Wait()
}

func downloadIt(url, outfile string) {
	defer func() {
		wg.Done()
	}()

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in downloadIt(): ", r)
		}
	}()

	var _, err = os.Stat(outfile)
	if err == nil {
		fmt.Printf("Ignore existed: %v\n", outfile)
		return
	} else {
		defer log.Printf("%v => %v\n", url, outfile)
	}

	if multiParts {
		multiRangeDownload(url, outfile)
	} else {
		downloadAsOne(url, outfile)
	}
}

func downloadAsOne(url, out string) {
	resp, err := client.Get(url)

	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		log.Println("Trouble making GET request!")
		return
	}

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Trouble reading reesponse body!")
		return
	}

	err = ioutil.WriteFile(out, contents, 0644)
	if err != nil {
		log.Println("Trouble creating file!")
		return
	}
}

func multiRangeDownload(url, out string) {
	outfile, err := os.Create(out)
	if err != nil {
		log.Println(err)
		return
	}
	defer outfile.Close()

	FileDownloader, err := NewFileDownloader(url, outfile, -1)
	if err != nil {
		log.Println(err)
		return
	}

	var _wg sync.WaitGroup
	var exit = make(chan bool)

	FileDownloader.OnFinish(func() {
		exit <- true
	})

	FileDownloader.OnError(func(errCode int, err error) {
		log.Println(errCode, err)
	})

	FileDownloader.OnStart(func() {
		log.Printf("Start download %v\n", out)
		format := "\r%12d/%v [%s] %9d kB/s %v"
		var lastSpeed int64
		for {
			status := FileDownloader.GetStatus()
			i := float64(status.Downloaded) / float64(FileDownloader.Size) * 50
			h := strings.Repeat("=", int(i)) + strings.Repeat(" ", 50-int(i))

			select {
			case <-exit:
				fmt.Printf(format, status.Downloaded, FileDownloader.Size, h, lastSpeed, "[ FINISHED! ]\n")
				os.Stdout.Sync()
				_wg.Done()
				// wait _wg to end
				time.Sleep(time.Second * 1)
			default:
				lastSpeed = status.Speeds / 1024
				fmt.Printf(format, status.Downloaded, FileDownloader.Size, h, lastSpeed, "[DOWNLOADING]")
				os.Stdout.Sync()
				time.Sleep(time.Second * 1)
			}
		}
	})

	_wg.Add(1)
	FileDownloader.Start()
	_wg.Wait()
}
