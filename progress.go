package mpb

import (
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/james-antill/mpb/cwriter"
	"github.com/james-antill/mpb/decor"
)

type (
	// BeforeRender is a func, which gets called before render process
	BeforeRender func([]*Bar)

	widthSync struct {
		Listen []chan int
		Result []chan int
	}

	// progress config, fields are adjustable by user indirectly
	pConf struct {
		bars []*Bar

		width        int
		format       string
		fmtFill      []string
		rr           time.Duration
		ewg          *sync.WaitGroup
		cw           *cwriter.Writer
		ticker       *time.Ticker
		beforeRender BeforeRender
		interceptors []func(io.Writer)

		shutdownNotifier chan struct{}
		cancel           <-chan struct{}
	}
)

const (
	// default RefreshRate
	prr = 100 * time.Millisecond
	// default width
	pwidth = 80
	// default format
	pformat = "[=> ]"
	// Do we want to try to use the utf8 progressbar
	utf8Fill = true
)

var (
	pmultiFillASCII = [2]string{"-", "="}
	// http://www.fileformat.info/info/unicode/block/block_elements/utf8test.htm
	pmultiFillUTF8 = [8]string{
		"\xe2\x96\x8f",
		"\xe2\x96\x8e",
		"\xe2\x96\x8d",
		"\xe2\x96\x8c",
		"\xe2\x96\x8b",
		"\xe2\x96\x8a",
		"\xe2\x96\x89",
		"\xe2\x96\x88"}
)

// Progress represents the container that renders Progress bars
type Progress struct {
	// wg for internal rendering sync
	wg *sync.WaitGroup
	// External wg
	ewg *sync.WaitGroup

	// quit channel to request p.server to quit
	quit chan struct{}
	// done channel is receiveable after p.server has been quit
	done chan struct{}
	ops  chan func(*pConf)
}

// Default sort the completed bars away, up the screen,
// also sort priority/ID higher as lower down the screen.
func defaultSort(bs []*Bar) {
	sort.SliceStable(bs, func(i, j int) bool {
		if bs[i].ID() != bs[j].ID() {
			return bs[i].ID() < bs[j].ID()
		}

		// Move the finished bars to the top...
		if bs[i].Total() != bs[i].Current() {
			return false
		}
		if bs[j].Total() == bs[j].Current() {
			return false
		}
		return true
	})
}

// New creates new Progress instance, which orchestrates bars rendering process.
// Accepts mpb.ProgressOption funcs for customization.
func New(options ...ProgressOption) *Progress {
	// This is ugly, but it's what python does for stdout.encoding.
	var fill = pmultiFillASCII[:]
	if utf8Fill && strings.HasSuffix(strings.ToLower(os.Getenv("LANG")), ".utf-8") {
		fill = pmultiFillUTF8[:]
	}

	// defaults
	conf := pConf{
		bars:         make([]*Bar, 0, 3),
		beforeRender: defaultSort,
		width:        pwidth,
		format:       pformat,
		fmtFill:      fill,
		cw:           cwriter.New(os.Stderr),
		rr:           prr,
		ticker:       time.NewTicker(prr),
	}

	for _, opt := range options {
		opt(&conf)
	}

	p := &Progress{
		ewg:  conf.ewg,
		wg:   new(sync.WaitGroup),
		done: make(chan struct{}),
		ops:  make(chan func(*pConf)),
		quit: make(chan struct{}),
	}
	go p.server(conf)
	return p
}

// AddBar creates a new progress bar and adds to the container.
func (p *Progress) AddBar(total int64, options ...BarOption) *Bar {
	result := make(chan *Bar, 1)
	op := func(c *pConf) {
		options = append(options, barWidth(c.width))
		options = append(options, barFormat(c.format, c.fmtFill))
		b := newBar(total, p.wg, c.cancel, options...)
		c.bars = append(c.bars, b)
		p.wg.Add(1)
		result <- b
	}
	select {
	case p.ops <- op:
		return <-result
	case <-p.quit:
		return new(Bar)
	}
}

// AddBarDef creates a new progress bar with sane default options.
func (p *Progress) AddBarDef(total int64, name string, unit decor.Units,
	options ...BarOption) *Bar {
	var opts []BarOption
	opts = append(opts, PrependDecorators(
		decor.StaticName(name, 0, 0),
		decor.DefDataPreBar(unit)))
	opts = append(opts, AppendDecorators(decor.ETA(4, decor.DwidthSync)))
	opts = append(opts, options...)
	return p.AddBar(total, opts...)
}

// RemoveBar removes bar at any time.
func (p *Progress) RemoveBar(b *Bar) bool {
	result := make(chan bool, 1)
	op := func(c *pConf) {
		var ok bool
		for i, bar := range c.bars {
			if bar == b {
				bar.Complete()
				c.bars = append(c.bars[:i], c.bars[i+1:]...)
				ok = true
				break
			}
		}
		result <- ok
	}
	select {
	case p.ops <- op:
		return <-result
	case <-p.quit:
		return false
	}
}

// BarCount returns bars count
func (p *Progress) BarCount() int {
	result := make(chan int, 1)
	op := func(c *pConf) {
		result <- len(c.bars)
	}
	select {
	case p.ops <- op:
		return <-result
	case <-p.quit:
		return 0
	}
}

// Stop is a way to gracefully shutdown mpb's rendering goroutine.
// It is NOT for cancelation (use mpb.WithContext for cancelation purposes).
// If *sync.WaitGroup has been provided via mpb.WithWaitGroup(), its Wait()
// method will be called first.
func (p *Progress) Stop() {
	if p.ewg != nil {
		p.ewg.Wait()
	}
	select {
	case <-p.quit:
		return
	default:
		// complete Total unknown bars
		p.ops <- func(c *pConf) {
			for _, b := range c.bars {
				b.complete()
			}
		}
		// wait for all bars to quit
		p.wg.Wait()
		// request p.server to quit
		p.quitRequest()
		// wait for p.server to quit
		<-p.done
	}
}

func (p *Progress) quitRequest() {
	select {
	case <-p.quit:
	default:
		close(p.quit)
	}
}

// server monitors underlying channels and renders any progress bars
func (p *Progress) server(conf pConf) {

	defer func() {
		if conf.shutdownNotifier != nil {
			close(conf.shutdownNotifier)
		}
		close(p.done)
	}()

	for {
		select {
		case op := <-p.ops:
			op(&conf)
		case <-conf.ticker.C:
			numBars := len(conf.bars)
			if numBars == 0 {
				break
			}

			if conf.beforeRender != nil {
				conf.beforeRender(conf.bars)
			}

			wSyncTimeout := make(chan struct{})
			time.AfterFunc(conf.rr, func() {
				close(wSyncTimeout)
			})

			tw, th, _ := cwriter.GetTermSize()
			// Default terminal is 80x24.
			if th < 4 { // Need 1 line of context and one blank at the bottom
				th = 24
			}
			if tw < 20 { // FIXME: Should count/size prependers
				tw = 80
			}

			// We want the last N bars, if we have too many it screws up
			// the terminal display (and is unreadable anyway)...
			bars := conf.bars[:]
			skip := 0
			th -= 3
			if numBars > th {
				skip = numBars - th
			}

			b0 := bars[0]
			prependWs := newWidthSync(wSyncTimeout, numBars, b0.NumOfPrependers())
			appendWs := newWidthSync(wSyncTimeout, numBars, b0.NumOfAppenders())

			flushed := make(chan struct{})
			sequence := make([]<-chan []byte, numBars)
			for i, b := range bars {
				b.Update()
				sequence[i] = b.render(tw, flushed, prependWs, appendWs)
			}

			for buf := range fanIn(skip, sequence...) {
				conf.cw.Write(buf)
			}

			for _, interceptor := range conf.interceptors {
				interceptor(conf.cw)
			}

			conf.cw.Flush()
			close(flushed)
		case <-conf.cancel:
			conf.ticker.Stop()
			conf.cancel = nil
		case <-p.quit:
			if conf.cancel != nil {
				conf.ticker.Stop()
			}
			return
		}
	}
}

func newWidthSync(timeout <-chan struct{}, numBars, numColumn int) *widthSync {
	ws := &widthSync{
		Listen: make([]chan int, numColumn),
		Result: make([]chan int, numColumn),
	}
	for i := 0; i < numColumn; i++ {
		ws.Listen[i] = make(chan int, numBars)
		ws.Result[i] = make(chan int, numBars)
	}
	for i := 0; i < numColumn; i++ {
		go func(listenCh <-chan int, resultCh chan<- int) {
			defer close(resultCh)
			widths := make([]int, 0, numBars)
		loop:
			for {
				select {
				case w := <-listenCh:
					widths = append(widths, w)
					if len(widths) == numBars {
						break loop
					}
				case <-timeout:
					if len(widths) == 0 {
						return
					}
					break loop
				}
			}
			result := max(widths)
			for i := 0; i < len(widths); i++ {
				resultCh <- result
			}
		}(ws.Listen[i], ws.Result[i])
	}
	return ws
}

func fanIn(skip int, inputs ...<-chan []byte) <-chan []byte {
	ch := make(chan []byte)

	go func() {
		defer close(ch)
		for _, input := range inputs {
			data := <-input
			if skip > 1 {
				skip--
				continue
			}
			ch <- data
		}
	}()

	return ch
}

func max(slice []int) int {
	max := slice[0]

	for i := 1; i < len(slice); i++ {
		if slice[i] > max {
			max = slice[i]
		}
	}

	return max
}
