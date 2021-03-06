package mpb

import (
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/james-antill/mpb/decor"
	"github.com/mattn/go-runewidth"
)

const (
	rLeft = iota
	rFill
	rTip
	rEmpty
	rRight
)

const (
	formatLen = 5
	etaAlpha  = 0.25
)

type fmtRunes [formatLen]rune
type fmtByteSegments [][]byte
type fmtFillSegments []rune

// Bar represents a progress Bar
type Bar struct {
	// quit channel to request b.server to quit
	quit chan struct{}
	// done channel is receiveable after b.server has been quit
	done chan struct{}
	ops  chan func(*state)

	// following are used after b.done is receiveable
	cacheState state
}

const rollAveSlots = 8
const rollAveTime = 2 * time.Second

type (
	refill struct {
		char rune
		till int64
	}
	state struct {
		id             int
		width          int
		format         fmtRunes
		fmtFill        []rune
		etaAlpha       float64
		total          int64
		current        int64
		trimLeftSpace  bool
		trimRightSpace bool
		started        bool
		completed      bool
		aborted        bool

		// Statistics ...
		startTime time.Time
		// For rolling average ETA
		rollTime  [rollAveSlots]time.Time
		rollTotal [rollAveSlots]int64
		rollOff   int

		appendFuncs   []decor.DecoratorFunc
		prependFuncs  []decor.DecoratorFunc
		simpleSpinner func() byte
		refill        *refill
	}
)

func newBar(total int64, wg *sync.WaitGroup, cancel <-chan struct{}, options ...BarOption) *Bar {
	s := state{
		total:    total,
		etaAlpha: etaAlpha,
	}

	if total <= 0 {
		s.simpleSpinner = getSpinner()
	}

	for _, opt := range options {
		opt(&s)
	}

	b := &Bar{
		quit: make(chan struct{}),
		done: make(chan struct{}),
		ops:  make(chan func(*state)),
	}

	go b.server(s, wg, cancel)

	return b
}

// RemoveAllPrependers removes all prepend functions
func (b *Bar) RemoveAllPrependers() {
	select {
	case b.ops <- func(s *state) {
		s.prependFuncs = nil
	}:
	case <-b.quit:
		return
	}
}

// RemoveAllAppenders removes all append functions
func (b *Bar) RemoveAllAppenders() {
	select {
	case b.ops <- func(s *state) {
		s.appendFuncs = nil
	}:
	case <-b.quit:
		return
	}
}

// ProxyReader wrapper for io operations, like io.Copy
func (b *Bar) ProxyReader(r io.Reader) *Reader {
	return &Reader{r, b}
}

// Increment shorthand for b.Incr(1)
func (b *Bar) Increment() {
	b.Incr(1)
}

// Update updates the startTime/timeElapsed for ETA/Nsec
func (b *Bar) Update() {
	b.Incr(0)
}

// Incr increments progress bar
func (b *Bar) Incr(n int) {
	if n < 0 {
		return
	}
	select {
	case b.ops <- func(s *state) {
		if s.current == 0 && !s.started {
			s.startTime = time.Now()
			s.initETA()
			s.started = true
		}
		sum := s.current + int64(n)
		s.updateETA(int64(n))
		if s.total > 0 && sum >= s.total {
			s.current = s.total
			s.completed = true
			return
		}
		s.current = sum
	}:
	case <-b.quit:
		return
	}
}

// ResumeFill fills bar with different r rune,
// from 0 to till amount of progress.
func (b *Bar) ResumeFill(r rune, till int64) {
	if till < 1 {
		return
	}
	select {
	case b.ops <- func(s *state) {
		s.refill = &refill{r, till}
	}:
	case <-b.quit:
		return
	}
}

func (b *Bar) NumOfAppenders() int {
	result := make(chan int, 1)
	select {
	case b.ops <- func(s *state) { result <- len(s.appendFuncs) }:
		return <-result
	case <-b.done:
		return len(b.cacheState.appendFuncs)
	}
}

func (b *Bar) NumOfPrependers() int {
	result := make(chan int, 1)
	select {
	case b.ops <- func(s *state) { result <- len(s.prependFuncs) }:
		return <-result
	case <-b.done:
		return len(b.cacheState.prependFuncs)
	}
}

// ID returs id of the bar
func (b *Bar) ID() int {
	result := make(chan int, 1)
	select {
	case b.ops <- func(s *state) { result <- s.id }:
		return <-result
	case <-b.done:
		return b.cacheState.id
	}
}

func (b *Bar) Current() int64 {
	result := make(chan int64, 1)
	select {
	case b.ops <- func(s *state) { result <- s.current }:
		return <-result
	case <-b.done:
		return b.cacheState.current
	}
}

func (b *Bar) Total() int64 {
	result := make(chan int64, 1)
	select {
	case b.ops <- func(s *state) { result <- s.total }:
		return <-result
	case <-b.done:
		return b.cacheState.total
	}
}

// InProgress returns true, while progress is running.
// Can be used as condition in for loop
func (b *Bar) InProgress() bool {
	select {
	case <-b.quit:
		return false
	default:
		return true
	}
}

// Complete signals to the bar, that process has been completed.
// You should call this method when total is unknown and you've reached the point
// of process completion. If you don't call this method, it will be called
// implicitly, upon p.Stop() call.
func (b *Bar) Complete() {
	select {
	case <-b.quit:
	default:
		close(b.quit)
	}
}

func (b *Bar) complete() {
	select {
	case b.ops <- func(s *state) {
		if !s.completed {
			b.Complete()
		}
	}:
	case <-time.After(prr):
		return
	}
}

func (b *Bar) server(s state, wg *sync.WaitGroup, cancel <-chan struct{}) {

	defer func() {
		b.cacheState = s
		close(b.done)
		wg.Done()
	}()

	for {
		select {
		case op := <-b.ops:
			op(&s)
		case <-b.quit:
			s.completed = true
			return
		case <-cancel:
			s.aborted = true
			cancel = nil
			b.Complete()
		}
	}
}

func (b *Bar) render(tw int, flushed chan struct{}, prependWs, appendWs *widthSync) <-chan []byte {
	ch := make(chan []byte, 1)

	go func() {
		defer func() {
			// recovering if external decorators panic
			if p := recover(); p != nil {
				ch <- []byte(fmt.Sprintln(p))
			}
			close(ch)
		}()
		var st state
		result := make(chan state, 1)
		select {
		case b.ops <- func(s *state) {
			result <- *s
			if s.completed {
				<-flushed
				b.Complete()
			}
		}:
			st = <-result
		case <-b.done:
			st = b.cacheState
		}
		buf := draw(&st, tw, prependWs, appendWs)
		buf = append(buf, '\n')
		ch <- buf
	}()

	return ch
}

func (s *state) updateFormat(format string, fillFmt []string) {
	for i, n := 0, 0; len(format) > 0; i++ {
		s.format[i], n = utf8.DecodeRuneInString(format)
		format = format[n:]
	}

	if len(fillFmt) < 1 {
		return
	}

	s.fmtFill = make([]rune, len(fillFmt))
	for i, f := range fillFmt {
		s.fmtFill[i], _ = utf8.DecodeRuneInString(f)
	}
	s.format[rFill] = s.fmtFill[len(s.fmtFill)-1]
}

func (s *state) initETA() {
	s.rollTime[0] = s.startTime
}

func (s *state) updateETA(amount int64) {
	if amount == 0 {
		return
	}

	dur := time.Since(s.rollTime[s.rollOff])
	if dur > rollAveTime {
		s.rollOff = (s.rollOff + 1) % rollAveSlots
		s.rollTime[s.rollOff] = time.Now()
		s.rollTotal[s.rollOff] = 0
	}

	s.rollTotal[s.rollOff] += amount
}

func (s *state) getDataETA() (time.Time, int64) {
	off := s.rollOff
	off = (off + 1) % rollAveSlots
	beg := s.rollTime[off] // Oldest time is the next to be used.
	cur := s.rollTotal[off]

	if cur == 0 { // Only happens when we haven't rolled over yet
		// Go with the main data...
		return s.startTime, s.current
	}

	for i := 1; i < rollAveSlots; i++ {
		off = (off + 1) % rollAveSlots
		cur += s.rollTotal[off]
	}

	return beg, cur
}

func draw(s *state, termWidth int, prependWs, appendWs *widthSync) []byte {
	if len(s.prependFuncs) != len(prependWs.Listen) || len(s.appendFuncs) != len(appendWs.Listen) {
		return []byte{}
	}
	if termWidth <= 0 {
		termWidth = s.width
	}

	stat := newStatistics(s)

	// render prepend functions to the left of the bar
	var prependBlock []byte
	for i, f := range s.prependFuncs {
		prependBlock = append(prependBlock,
			[]byte(f(stat, prependWs.Listen[i], prependWs.Result[i]))...)
	}

	// render append functions to the right of the bar
	var appendBlock []byte
	for i, f := range s.appendFuncs {
		appendBlock = append(appendBlock,
			[]byte(f(stat, appendWs.Listen[i], appendWs.Result[i]))...)
	}

	prependCount := utf8.RuneCount(prependBlock)
	appendCount := utf8.RuneCount(appendBlock)

	var leftSpace, rightSpace []byte
	space := []byte{' '}

	if !s.trimLeftSpace {
		prependCount++
		leftSpace = space
	}
	if !s.trimRightSpace {
		appendCount++
		rightSpace = space
	}

	var barBlock []byte
	buf := make([]byte, 0, termWidth)
	segments := fmtRunesToByteSegments(s.format[:])
	fmtFill := fmtRunesToByteSegments(s.fmtFill)

	if s.simpleSpinner != nil {
		for _, block := range [...][]byte{segments[rLeft], {s.simpleSpinner()}, segments[rRight]} {
			barBlock = append(barBlock, block...)
		}
	} else {
		barBlock = fillBar(s.total, s.current, s.width, segments,
			fmtFill, s.refill)
		barCount := runewidth.StringWidth(string(barBlock))
		totalCount := prependCount + barCount + appendCount
		if totalCount > termWidth {
			shrinkWidth := termWidth - prependCount - appendCount
			barBlock = fillBar(s.total, s.current, shrinkWidth, segments,
				fmtFill, s.refill)
		}
	}

	return concatenateBlocks(buf, prependBlock, leftSpace, barBlock, rightSpace, appendBlock)
}

func concatenateBlocks(buf []byte, blocks ...[]byte) []byte {
	for _, block := range blocks {
		buf = append(buf, block...)
	}
	return buf
}

func fillBar(total, current int64, width int,
	fmtBytes, fmtFill fmtByteSegments, rf *refill) []byte {
	if width < 2 || total <= 0 {
		return []byte{}
	}

	// bar width without leftEnd and rightEnd runes
	barWidth := width - 2

	buf := make([]byte, 0, width)

	// When we get to 100% don't leave bar droppings
	if current >= total {
		barWidth += 2
		for i := 0; i < barWidth; i++ {
			buf = append(buf, fmtBytes[rEmpty]...)
		}
		return buf
	}

	flen := len(fmtFill)
	completedWidth, foff := decor.CalcPercentage(total, current, barWidth, flen)

	buf = append(buf, fmtBytes[rLeft]...)

	if rf != nil {
		till, _ := decor.CalcPercentage(total, rf.till, barWidth, 0)
		rbytes := make([]byte, utf8.RuneLen(rf.char))
		utf8.EncodeRune(rbytes, rf.char)
		// append refill rune
		for i := 0; i < till; i++ {
			buf = append(buf, rbytes...)
		}
		for i := till; i < completedWidth; i++ {
			buf = append(buf, fmtBytes[rFill]...)
		}
	} else {
		for i := 0; i < completedWidth; i++ {
			buf = append(buf, fmtBytes[rFill]...)
		}
	}

	if flen >= 1 {
		if foff >= 1 {
			buf = append(buf, fmtFill[foff-1]...)
			completedWidth++
		}
	} else if completedWidth < barWidth && completedWidth > 0 {
		_, size := utf8.DecodeLastRune(buf)
		buf = buf[:len(buf)-size]
		buf = append(buf, fmtBytes[rTip]...)
	}

	for i := completedWidth; i < barWidth; i++ {
		buf = append(buf, fmtBytes[rEmpty]...)
	}

	buf = append(buf, fmtBytes[rRight]...)

	return buf
}

func newStatistics(s *state) *decor.Statistics {
	beg, cur := s.getDataETA()

	return &decor.Statistics{
		ID:          s.id,
		Completed:   s.completed,
		Aborted:     s.aborted,
		Total:       s.total,
		Current:     s.current,
		StartTime:   s.startTime,
		TimeElapsed: time.Since(s.startTime),

		RollCurrent:   cur,
		RollStartTime: beg,
	}
}

func fmtRunesToByteSegments(format []rune) fmtByteSegments {
	segments := make(fmtByteSegments, len(format))
	for i, r := range format {
		buf := make([]byte, utf8.RuneLen(r))
		utf8.EncodeRune(buf, r)
		segments[i] = buf
	}
	return segments
}

func getSpinner() func() byte {
	chars := []byte(`-\|/`)
	repeat := len(chars) - 1
	index := repeat
	return func() byte {
		if index == repeat {
			index = -1
		}
		index++
		return chars[index]
	}
}
