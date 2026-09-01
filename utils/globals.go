package utils

import "os"

var GlobalDebugFlag bool

var StdoutIsTerminal = charDevice(os.Stdout)

func charDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
