package main

import (
	"fmt"
	"github.com/pixelrazor/kfi"
	"os"
)

func main() {
	if len(os.Args) == 1 {
		fmt.Println("Error: please supply a PID (", os.Args[0], "<PID> )")
		os.Exit(-1)
	}
	res, err := kfi.Inject(os.Args[1])
	if err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
	fmt.Println(res)
}