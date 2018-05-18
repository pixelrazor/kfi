// Package kfi provides a wrapper for the kfi module
package kfi

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
)

// Inject a fault into a running program.
// A random register is picked, and a random bit in that register is flipped.
func Inject(pid string) (string, error) {
	if _, err := os.Stat("/proc/kfi"); os.IsNotExist(err) {
		return "", errors.New("Error: kfi proc entry is not found, please ensure the module has been inserted")
	}
	kfi, err := os.OpenFile("/proc/kfi", os.O_RDWR, 0666)
	if err != nil {
		return "", errors.New("Error opening /proc/kfi: " + err.Error())
	}
	defer kfi.Close()
	if _, err := kfi.Write([]byte(pid)); err != nil {
		return "", errors.New("Error reading from /proc/kfi, please see dmesg for details")
	}
	buff, err := ioutil.ReadAll(kfi)
	if err != nil {
		return "", errors.New("Error reading from /proc/kfi, please see dmesg for details")
	}
	return string(buff), nil
}

// Convenience function to use 'int' instead of 'string'
func InjectByInt(pid int) (string, error) {
	return Inject(fmt.Sprintf("%v", pid))
}
