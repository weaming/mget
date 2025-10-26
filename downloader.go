// https://github.com/iovxw/downloader

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	MaxThread   = 16
	CacheSize   = 1024
	MaxRetries  = 3
	HTTPTimeout = 20
)

var (
	sizeNotMatch = errors.New("size not match")

	httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 30,
		},
		Timeout: HTTPTimeout * time.Second,
	}
)

type Status struct {
	sync.RWMutex
	Downloaded int64
	Speeds     int64
}

func (s *Status) WithLock(fn func(), write bool) {
	if write {
		s.Lock()
		defer s.Unlock()
	} else {
		s.RLock()
		defer s.RUnlock()
	}
	fn()
}

type Block struct {
	Begin int64
	End   int64
}

type FileDownloader struct {
	Url  string   // 下载地址
	Size int64    // 文件大小
	File *os.File // 要写入的文件

	BlockList []Block // 用于记录未下载的文件块起始位置
	Status    *Status

	onStart  func()
	onFinish func()
	onError  func(error)

	stopChan chan struct{}
}

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

	dl := &FileDownloader{
		Url:      url,
		Size:     size,
		File:     file,
		Status:   &Status{Downloaded: 0, Speeds: 0},
		stopChan: make(chan struct{}),
	}

	return dl, nil
}

// see https://tools.ietf.org/html/rfc7233#section-2.1
// The last-byte-pos value gives the byte-offset of the last byte in the range;
// that is, the byte positions specified are inclusive.
// Byte offsets start at zero.
func (f *FileDownloader) Start() {
	if f.Size <= 0 {
		f.BlockList = append(f.BlockList, Block{0, -1})
		f.Size = 1
	} else {
		blockSize := f.Size / int64(MaxThread)
		// 数据平均分配给各个线程
		for i := 0; i < MaxThread; i++ {
			begin := blockSize * (int64(i))
			end := begin + blockSize - 1
			f.BlockList = append(f.BlockList, Block{begin, end})
		}
		// 将余出数据分配给最后一个线程
		f.BlockList[MaxThread-1].End = f.Size - 1
	}

	f.emit(f.onStart)
	// 开始下载
	f.download()
}

func (f *FileDownloader) download() {
	go f.updateSpeeds()

	wg := new(sync.WaitGroup)
	wg.Add(len(f.BlockList))
	for i := range f.BlockList {
		go func(id int) {
			defer func() {
				wg.Done()
			}()

			retries := 0
			for retries < MaxRetries {
				err := f.downloadBlock(id)
				if err != nil {
					retries++
					f.emitErr(fmt.Errorf("block %d download failed (attempt %d/%d): %w", id, retries, MaxRetries, err))
					if retries >= MaxRetries {
						return
					}
					time.Sleep(time.Second * time.Duration(retries))
					continue
				}
				return
			}
		}(i)
	}

	wg.Wait()
	close(f.stopChan)
	f.emit(f.onFinish)
}

// 文件块下载器
// 根据线程ID获取下载块的起始位置
func (f *FileDownloader) downloadBlock(id int) error {
	begin := f.BlockList[id].Begin
	end := f.BlockList[id].End

	request, err := http.NewRequest("GET", f.Url, nil)
	if err != nil {
		return err
	}

	if end != -1 {
		rangeHeader := "bytes=" + strconv.FormatInt(begin, 10) + "-" + strconv.FormatInt(end, 10)
		request.Header.Set("Range", rangeHeader)
	}

	resp, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var buf = make([]byte, CacheSize)
	for {
		n, e := resp.Body.Read(buf)

		bufSize := int64(len(buf[:n]))
		if end != -1 {
			sizeNeeds := end - begin + 1
			// 检查下载的大小是否超出需要下载的大小
			if bufSize > sizeNeeds {
				// 数据大小不正常
				// 一般是因为网络环境不好导致
				// 比如用中国电信下载国外文件

				// 设置数据大小来去掉多余数据
				// 并结束这个线程的下载
				bufSize = sizeNeeds
				n = int(sizeNeeds)
				e = io.EOF
			}
		}
		if bufSize > 0 {
			// 将缓冲数据写入硬盘
			_, writeErr := f.File.WriteAt(buf[:bufSize], begin)
			if writeErr != nil {
				return fmt.Errorf("write to file failed: %w", writeErr)
			}

			// 更新已下载大小
			f.Status.WithLock(func() {
				f.Status.Downloaded += bufSize
			}, true)
			begin += bufSize
		}

		if e != nil {
			if e == io.EOF {
				// 数据已经下载完毕
				return nil
			}
			return e
		}
	}
}

func (f *FileDownloader) HumanSize() string {
	units := []string{"bytes", "KB", "MB", "GB", "PB"}
	tmp := float64(f.Size)
	for _, unit := range units {
		if tmp < 1024 {
			return fmt.Sprintf("%.3f %v", tmp, unit)
		}
		tmp = tmp / 1024
	}
	return fmt.Sprintf("%v %v", tmp, "???")
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
func (f *FileDownloader) emit(fn func()) {
	if fn != nil {
		go fn()
	}
}

// 触发Error事件
func (f *FileDownloader) emitErr(err error) {
	if f.onError != nil {
		go f.onError(err)
	}
}
func (f *FileDownloader) updateSpeeds() {
	f.Status.RLock()
	old := f.Status.Downloaded
	f.Status.RUnlock()

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopChan:
			return
		case <-ticker.C:
			f.Status.WithLock(func() {
				new := f.Status.Downloaded
				f.Status.Speeds = (new - old) * 10
				old = new
			}, true)
		}
	}
}
