package decor

import "fmt"

const (
	_   = iota
	KiB = 1 << (iota * 10)
	MiB
	GiB
	TiB
)

const (
	KB = 1000
	MB = KB * 1000
	GB = MB * 1000
	TB = GB * 1000
)

const (
	_ = iota
	// Unit_KiB Kibibyte = 1024 b
	Unit_KiB
	// Unit_kB Kilobyte = 1000 b
	Unit_kB
	// Unit_k Kibibyte = 1000
	Unit_k
)

// Unit_Kb Kilobyte = 1000 b
const Unit_Kb = Unit_kB

// Unit_KB Kilobyte = 1000 b
const Unit_KB = Unit_kB

type Units uint

func Format(i int64) *formatter {
	return &formatter{n: i}
}

func FormatF(i float64) *formatterF {
	return &formatterF{n: i}
}

type formatter struct {
	n     int64
	unit  Units
	width int
}

type formatterF struct {
	n     float64
	unit  Units
	width int
}

func (f *formatter) To(unit Units) *formatter {
	f.unit = unit
	return f
}

func (f *formatter) Width(width int) *formatter {
	f.width = width
	return f
}

func (f *formatter) String() string {
	switch f.unit {
	case Unit_KiB:
		return formatKiB(f.n)
	case Unit_kB:
		return formatKB(f.n)
	case Unit_k:
		return formatK(f.n)
	default:
		return fmt.Sprintf(fmt.Sprintf("%%%dd", f.width), f.n)
	}
}

func (f *formatterF) To(unit Units) *formatterF {
	f.unit = unit
	return f
}

func (f *formatterF) Width(width int) *formatterF {
	f.width = width
	return f
}

func (f *formatterF) String() string {
	switch f.unit {
	case Unit_KiB:
		return formatFKiB(f.n)
	case Unit_kB:
		return formatFKB(f.n)
	case Unit_k:
		return formatFK(f.n)
	default:
		return fmt.Sprintf(fmt.Sprintf("%%%d.2f", f.width), f.n)
	}
}

// round use like so: "%.1f", round(f, 0.1) or "%.0f", round(f, 1)
// Otherwise 9.9999 is < 10 but "%.1f" will give "10.0"
func round(x, unit float64) float64 {
	return float64(int64(x/unit+0.5)) * unit
}

// What we want is useful level of information. Eg.
// 999b
// 1.2KB
//  22KB
// 222KB
// 1.2MB

func fmtSprint(f float64, ext string) string {
	if round(f, 0.1) >= 10 {
		return fmt.Sprintf("%3d%s", int(f), ext)
	}
	return fmt.Sprintf("%.1f%s", f, ext)
}

func formatFKiB(f float64) string {
	ext := "b  "
	switch {
	case f >= TiB:
		f /= TiB
		ext = "TiB"
	case f >= GiB:
		f /= GiB
		ext = "GiB"
	case f >= MiB:
		f /= MiB
		ext = "MiB"
	case f >= KiB:
		f /= KiB
		ext = "KiB"
	}
	return fmtSprint(f, ext)
}
func formatKiB(i int64) string {
	return formatFKiB(float64(i))
}

func formatFKB(f float64) string {
	ext := "b "
	switch {
	case f >= TB:
		f /= TB
		ext = "TB"
	case f >= GB:
		f /= GB
		ext = "GB"
	case f >= MB:
		f /= MB
		ext = "MB"
	case f >= KB:
		f /= KB
		ext = "KB"
	}
	return fmtSprint(f, ext)
}
func formatKB(i int64) string {
	return formatFKB(float64(i))
}

func formatFK(f float64) string {
	ext := " "
	switch {
	case f >= TB:
		f /= TB
		ext = "T"
	case f >= GB:
		f /= GB
		ext = "G"
	case f >= MB:
		f /= MB
		ext = "M"
	case f >= KB:
		f /= KB
		ext = "K"
	}
	return fmtSprint(f, ext)
}
func formatK(i int64) string {
	return formatFK(float64(i))
}
