package main

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

var wg sync.WaitGroup
var outfile = ""
var multiParts = true
var exitbyuser = make(chan bool)
var exited = make(chan bool)

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

func isError(err error) bool {
	if err != nil {
		fmt.Println(err.Error())
	}

	return (err != nil)
}

func delete(path string) {
	var err = os.Remove(path)
	if isError(err) {
		return
	}

	log.Printf("deleted unfinished file: %v\n", path)
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
	}

	if multiParts {
		err = multiRangeDownload(url, outfile)
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
	} else {
		log.Printf("%v => %v\n", url, outfile)
		// download success
		wg.Done()
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
		exited <- true

		// delete it if blank
		if size == 0 {
			os.Remove(out)
		}
	}()

	FileDownloader, err := NewFileDownloader(url, outfile, -1)
	if err != nil {
		return err
	}

	var _wg sync.WaitGroup
	var exit = make(chan bool)
	go func() {
		// block until exit by user
		<-exitbyuser
		// notify downloader to exit
		FileDownloader.Pause()
		exit <- true
		err = errors.New("canceled by user!")
	}()

	FileDownloader.OnFinish(func() {
		exit <- true
	})

	FileDownloader.OnError(func(errCode int, err error) {
		log.Println(errCode, err)
	})

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
			case <-exit:
				fmt.Printf(format, status.Downloaded, FileDownloader.Size, h, lastSpeed, "[ FINISHED! ]\n")
				os.Stdout.Sync()
				_wg.Done()
				return
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
	return
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

	// handle the signal SIGINT
	c := make(chan os.Signal, 1)
	//signal.Notify(c, syscall.SIGINT, syscall.SIGQUIT)
	signal.Notify(c, syscall.SIGINT)

	// add, done if download success or exit explicityly
	wg.Add(1)
	go func() {
		for _ = range c {
			// captured the cancle signal
			exitbyuser <- true
			// wait for releasing the file
			<-exited
			delete(outfile)
			os.Exit(69)
		}
	}()

	wg.Add(1)
	go downloadIt(url, outfile)
	wg.Wait()
}
