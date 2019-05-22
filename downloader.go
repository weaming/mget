// https://github.com/iovxw/downloader

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	// 最大线程数量
	MaxThread = 8
	// 缓冲区大小
	CacheSize = 1024
)

// 创建新的文件下载
// 如果 size <= 0 则自动获取文件大小
func NewFileDownloader(url string, file *os.File, size int64) (*FileDownloader, error) {
	if size <= 0 {
		// 获取文件信息
		resp, err := http.Head(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		size = resp.ContentLength
	}

	if size <= 0 {
		return nil, errors.New("HTTP HEAD response without \"Content-Length\"")
	}

	f := &FileDownloader{
		Url:  url,
		Size: size,
		File: file,
	}

	return f, nil
}

type FileDownloader struct {
	Url  string   // 下载地址
	Size int64    // 文件大小
	File *os.File // 要写入的文件

	BlockList []Block // 用于记录未下载的文件块起始位置
	status    Status

	onStart  func()
	onFinish func()
	onError  func(error)
}

type Status struct {
	Downloaded int64
	Speeds     int64
}

type Block struct {
	Begin int64 `json:"begin"`
	End   int64 `json:"end"`
}

func (f *FileDownloader) Start() {
	go func() {
		if f.Size <= 0 {
			f.BlockList = append(f.BlockList, Block{0, -1})
			f.Size = 1
		} else {
			blockSize := f.Size / int64(MaxThread)
			// start from 0
			var begin int64
			// 数据平均分配给各个线程
			for i := 0; i < MaxThread; i++ {
				var end = (int64(i)+1)*blockSize - 1
				f.BlockList = append(f.BlockList, Block{begin, end})
				begin = end + 1
			}
			// 将余出数据分配给最后一个线程
			f.BlockList[MaxThread-1].End += f.Size - f.BlockList[MaxThread-1].End - 1
		}

		f.touch(f.onStart)
		// 开始下载
		err := f.download()
		if err != nil {
			f.touchOnError(err)
			return
		}
	}()
}

func (f *FileDownloader) download() error {
	f.startGetSpeeds()

	size := len(f.BlockList)
	ok := make(chan bool, size)
	for i := range f.BlockList {
		go func(id int) {
			defer func() {
				ok <- true
			}()

			for {
				err := f.downloadBlock(id)
				if err != nil {
					f.touchOnError(err)
					// 重新下载
					continue
				}
				break
			}
		}(i)
	}

	for i := 0; i < size; i++ {
		<-ok
	}
	f.touch(f.onFinish)
	return nil
}

// 文件块下载器
// 根据线程ID获取下载块的起始位置
func (f *FileDownloader) downloadBlock(id int) error {
	request, err := http.NewRequest("GET", f.Url, nil)
	if err != nil {
		return err
	}
	begin := f.BlockList[id].Begin
	end := f.BlockList[id].End
	if end != -1 {
		request.Header.Set(
			"Range",
			"bytes="+strconv.FormatInt(begin, 10)+"-"+strconv.FormatInt(end, 10),
		)
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var buf = make([]byte, CacheSize)
	for {
		n, e := resp.Body.Read(buf)

		// bufSize 可能小于 n 吗?
		bufSize := int64(len(buf[:n]))
		if end != -1 {
			// 检查下载的大小是否超出需要下载的大小
			needSize := f.BlockList[id].End + 1 - f.BlockList[id].Begin
			if bufSize > needSize {
				// 数据大小不正常
				// 一般是因为网络环境不好导致
				// 比如用中国电信下载国外文件

				// 设置数据大小来去掉多余数据
				// 并结束这个线程的下载
				bufSize = needSize
				n = int(needSize)
				e = io.EOF
			}
		}
		// 将缓冲数据写入硬盘
		f.File.WriteAt(buf[:bufSize], f.BlockList[id].Begin)

		// 更新已下载大小
		f.status.Downloaded += bufSize
		f.BlockList[id].Begin += bufSize

		if e != nil {
			if e == io.EOF {
				// 数据已经下载完毕
				return nil
			}
			return e
		}
	}

	return nil
}

func (f *FileDownloader) Status() Status {
	return f.status
}

func (f *FileDownloader) HumanSize() string {
	units := map[int]string{
		0: "bytes",
		1: "KB",
		2: "MB",
		3: "GB",
		4: "PB",
	}
	tmp := float64(f.Size)
	for i := 0; i <= 4; i++ {
		if tmp < 1024 {
			return fmt.Sprintf("%.3f %v", tmp, units[i])
		}
		tmp = tmp / 1024
	}
	return fmt.Sprintf("%v %v", tmp, "Boom")
}

// 任务开始时触发的事件
func (f *FileDownloader) OnStart(fn func()) {
	f.onStart = fn
}

// 任务完成时触发的事件
func (f *FileDownloader) OnFinish(fn func()) {
	f.onFinish = fn
}

// 任务出错时触发的事件
// errCode为错误码，errStr为错误描述
func (f *FileDownloader) OnError(fn func(error)) {
	f.onError = fn
}

// 用于触发事件
func (f FileDownloader) touch(fn func()) {
	if fn != nil {
		go fn()
	}
}

// 触发Error事件
func (f FileDownloader) touchOnError(err error) {
	if f.onError != nil {
		go f.onError(err)
	}
}
func (f *FileDownloader) startGetSpeeds() {
	go func() {
		var old = f.status.Downloaded
		for {
			time.Sleep(time.Second * 1)
			f.status.Speeds = f.status.Downloaded - old
			old = f.status.Downloaded
		}
	}()
}
