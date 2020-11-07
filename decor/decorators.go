package decor

import (
	"fmt"
	"time"

	runewidth "github.com/mattn/go-runewidth"
)

const (
	// DidentRight specifies identation direction.
	// |foo   |b     | With DidentRight
	// |   foo|     b| Without DidentRight
	DidentRight = 1 << iota

	// DwidthSync will auto sync max width.
	// Makes sense when there're more than one bar
	DwidthSync

	// DextraSpace adds extra space, makes sense with DwidthSync only.
	// When DidentRight bit set, the space will be added to the right,
	// otherwise to the left.
	DextraSpace

	// DSyncSpace is shortcut for DwidthSync|DextraSpace
	DSyncSpace = DwidthSync | DextraSpace
)

// Statistics represents statistics of the progress bar.
// Cantains: Total, Current, TimeElapsed, TimePerItemEstimate
type Statistics struct {
	ID                  int
	Completed           bool
	Aborted             bool
	Total               int64
	Current             int64
	StartTime           time.Time
	TimeElapsed         time.Duration
	TimePerItemEstimate time.Duration
	RollStartTime       time.Time
	RollCurrent         int64
}

// Eta moving-average ETA estimator
func (s *Statistics) Eta() time.Duration {
	timeElapsed := time.Since(s.RollStartTime)

	nsec := float64(s.RollCurrent) / timeElapsed.Seconds()
	eta := time.Duration(float64(s.Total-s.Current)/nsec) * time.Second
	return eta
}

// DecoratorFunc is a function that can be prepended and appended to the progress bar
type DecoratorFunc func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string

// Name deprecated, use StaticName instead
func Name(name string, minWidth int, conf byte) DecoratorFunc {
	return StaticName(name, minWidth, conf)
}

// StaticName to be used, when there is no plan to change the name during whole
// life of a progress rendering process
func StaticName(name string, minWidth int, conf byte) DecoratorFunc {
	nameFn := func(s *Statistics) string {
		return name
	}
	return DynamicName(nameFn, minWidth, conf)
}

// DynamicName to be used, when there is a plan to change the name once or
// several times during progress rendering process. If there're more than one
// bar, and you'd like to synchronize column width, conf param should have
// DwidthSync bit set.
func DynamicName(nameFn func(*Statistics) string, minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		name := nameFn(s)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(name)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), name)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), name)
	}
}

// Counters provides basic counters decorator.
// Accepts pairFormat string, something like "%s / %s" to be used in
// fmt.Sprintf(pairFormat, current, total) and one of (Unit_KiB/Unit_kB)
// constant. If there're more than one bar, and you'd like to synchronize column
// width, conf param should have DwidthSync bit set.
func CountersString(s *Statistics, pairFormat string, unit Units) string {
	current := Format(s.Current).To(unit)
	total := Format(s.Total).To(unit)
	str := fmt.Sprintf(pairFormat, current, total)
	return str
}
func Counters(pairFormat string, unit Units, minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := CountersString(s, pairFormat, unit)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(str)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), str)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), str)
	}
}

// Nsec provides basic Num/sec decorator.
// Accepts string, something like "%s/s" to be used in
// fmt.Sprintf(nsecformat, current) and one of (Unit_KiB/Unit_kB)
// constant. If there're more than one bar, and you'd like to synchronize column
// width, conf param should have DwidthSync bit set.
func NsecString(s *Statistics, nsecformat string, unit Units) string {
	var nsec float64
	if s.Current > 0 {
		timeElapsed := time.Since(s.RollStartTime)
		nsec = float64(s.RollCurrent) / timeElapsed.Seconds()
	}
	current := FormatF(nsec).To(unit)
	str := fmt.Sprintf(nsecformat, current)
	return str
}
func Nsec(nsecformat string, unit Units, minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := NsecString(s, nsecformat, unit)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(str)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), str)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), str)
	}
}

func smallDurationString(d time.Duration) string {

	switch {
	case d > 13*7*24*time.Hour:
		return ">13w"
	case d > 7*24*time.Hour:
		hours := int(d.Round(time.Hour).Hours())
		days := hours / 24
		weeks := days / 7
		days %= 7
		if days > 0 {
			return fmt.Sprintf("%dw%dd", weeks, days)
		} else {
			return fmt.Sprintf("%dw", weeks)
		}
	case d > 24*time.Hour:
		hours := int(d.Round(time.Hour).Hours())
		days := hours / 24
		hours %= 24
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		} else {
			return fmt.Sprintf("%dd", days)
		}
	case d > 8*time.Hour:
		return d.Round(time.Hour).String()
	case d > 8*time.Minute:
		return d.Round(time.Minute).String()
	case d > 8*time.Second:
		return d.Round(time.Second).String()
	default:
		return d.Round(100 * time.Millisecond).String()
	}
}

// ETA provides exponential-weighted-moving-average ETA decorator, shows the
// elapsed time after the progress has finished.
// If there're more than one bar, and you'd like to synchronize column width,
// conf param should have DwidthSync bit set.
func ETAString(s *Statistics) string {
	var dur time.Duration
	if s.Current == s.Total {
		return smallDurationString(s.TimeElapsed)
	} else {
		dur = s.Eta()
	}
	var str string
	secs := int(dur.Seconds()) % 60
	if s.RollCurrent == 0 {
		return "∞:??"
	} else if dur.Hours() > 999*24 {
		str = "∞"
	} else if dur.Hours() > 36 { // In theory this could be higher, but human UI
		d := dur.Round(time.Hour*24).Hours() / 24
		str = fmt.Sprintf("~%dd", int(d))
	} else if dur.Minutes() > 59 {
		h := dur.Round(time.Hour).Hours()
		str = fmt.Sprintf("~%dh", int(h))
	} else if dur.Seconds() < 3 {
		str = "~2s"
	} else {
		str = fmt.Sprintf("%d:%02d", int(dur.Minutes()), secs)
	}
	return str
}
func ETA(minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := ETAString(s)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(str)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), str)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), str)
	}
}

// Elapsed provides elapsed time decorator.
// If there're more than one bar, and you'd like to synchronize column width,
// conf param should have DwidthSync bit set.
func ElapsedString(s *Statistics) string {
	str := fmt.Sprint(time.Duration(s.TimeElapsed.Seconds()) * time.Second)
	return str
}
func Elapsed(minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := ElapsedString(s)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(str)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), str)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), str)
	}
}

// Percentage provides percentage decorator.
// If there're more than one bar, and you'd like to synchronize column width,
// conf param should have DwidthSync bit set.
func PercentageString(s *Statistics) string {
	str := "   "
	if s.Current > 0 && s.Current < s.Total {
		// Don't round up to 100%
		pc := (100 * s.Current) / s.Total
		str = fmt.Sprintf("%2d%%", pc)
	}
	return str
}
func Percentage(minWidth int, conf byte) DecoratorFunc {
	format := "%%"
	if (conf & DidentRight) != 0 {
		format += "-"
	}
	format += "%ds"
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := PercentageString(s)
		if (conf & DwidthSync) != 0 {
			myWidth <- runewidth.StringWidth(str)
			max := <-maxWidth
			if (conf & DextraSpace) != 0 {
				max++
			}
			return fmt.Sprintf(fmt.Sprintf(format, max), str)
		}
		return fmt.Sprintf(fmt.Sprintf(format, minWidth), str)
	}
}

func DefDataPreBar(unit Units) DecoratorFunc {
	return func(s *Statistics, myWidth chan<- int, maxWidth <-chan int) string {
		str := NsecString(s, "%s/s ", unit)
		str += CountersString(s, "%s%.0s", unit)
		pc := PercentageString(s)
		if pc != "" {
			str += " "
			str += pc
		}

		return str
	}
}

func CalcPercentage(total, current int64, width, fill int) (int, int) {
	if total == 0 || current > total {
		return 0, 0
	}
	num := float64(width) * float64(current) / float64(total)
	if fill > 0 {
		rem := num - float64(int(num))
		return int(num), int(rem / (1.0 / float64(fill)))
	}

	return int(round(num, 1)), 0
}
