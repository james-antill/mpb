package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/james-antill/mpb"
	"github.com/james-antill/mpb/decor"
)

const (
	totalBars    = 128
	liveBars     = 32
	maxBlockSize = 8
)

type BarData struct {
	name  string
	total int
}

func main() {

	var wg sync.WaitGroup
	p := mpb.New(mpb.WithWaitGroup(&wg))
	wg.Add(totalBars)

	wchan := make(chan struct{}, liveBars)
	go func() { wg.Wait(); close(wchan) }()
	for i := 0; i < liveBars; i++ {
		wchan <- struct{}{}
	}

	bars := make([]BarData, totalBars)
	totalData := 0
	for i := 0; i < totalBars; i++ {
		bars[i].name = fmt.Sprintf("Bar#%02d: ", i)
		bars[i].total = rand.Intn(10+i*3) + 10
		totalData += bars[i].total
	}

	tbbar := p.AddBarDef(int64(totalBars), "Bars: ", decor.Unit_k, mpb.BarID(2))
	tdbar := p.AddBarDef(int64(totalData), "Data: ", decor.Unit_k, mpb.BarID(3))

	for i := 0; i < totalBars; i++ {
		name := bars[i].name
		total := bars[i].total

		bar := p.AddBarDef(int64(total), name, decor.Unit_k)

		go func() {
			defer wg.Done()
			defer tbbar.Increment()
			defer func() { wchan <- struct{}{} }()

			blockSize := rand.Intn(maxBlockSize) + 1
			for i := 0; i < total; i++ {
				sleep(blockSize)
				bar.Increment()
				tdbar.Increment()
				blockSize = rand.Intn(maxBlockSize) + 1
			}
		}()
		<-wchan
	}
	for range wchan {
		continue
	}

	p.Stop()
	fmt.Println("stop")
}

func sleep(blockSize int) {
	time.Sleep(time.Duration(blockSize) * (50*time.Millisecond + time.Duration(rand.Intn(5*int(time.Millisecond)))))
}
